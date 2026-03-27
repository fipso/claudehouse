package terminal

import (
	"bytes"
	"claudehouse/sandbox"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type PTY struct {
	ptmx        *os.File
	cmd         *exec.Cmd
	output      chan []byte
	done        chan struct{}
	bundleDir   string              // non-empty for sandboxed PTYs
	stateDir    string              // runsc state dir
	containerID string
	pastaNetns  *sandbox.PastaNetns // non-nil for sandboxed PTYs with networking
}

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if u, err := user.Lookup(os.Getenv("USER")); err == nil && u.HomeDir != "" {
		// On NixOS, read shell from /etc/passwd via getent
		out, err := exec.Command("getent", "passwd", u.Username).Output()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(out)), ":")
			if len(parts) >= 7 && parts[6] != "" {
				return parts[6]
			}
		}
	}
	return "/bin/sh"
}

func SpawnPTY(shell string, cols, rows uint16, cellW, cellH int, extraEnv []string) (*PTY, error) {
	if shell == "" {
		shell = defaultShell()
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Env = append(cmd.Env, extraEnv...)

	winSize := &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    cols * uint16(cellW),
		Y:    rows * uint16(cellH),
	}

	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return nil, err
	}

	p := &PTY{
		ptmx:   ptmx,
		cmd:    cmd,
		output: make(chan []byte, 256),
		done:   make(chan struct{}),
	}

	go p.readLoop()
	return p, nil
}

func (p *PTY) readLoop() {
	defer close(p.done)
	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			p.output <- data
		}
		if err != nil {
			if err == io.EOF || isEIO(err) {
				return
			}
			return
		}
	}
}

func isEIO(err error) bool {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err == syscall.EIO
	}
	return false
}

func (p *PTY) Output() <-chan []byte {
	return p.output
}

func (p *PTY) Done() <-chan struct{} {
	return p.done
}

func (p *PTY) Write(data []byte) {
	_, _ = p.ptmx.Write(data)
}

func (p *PTY) Resize(cols, rows uint16, cellW, cellH int) {
	pty.Setsize(p.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    cols * uint16(cellW),
		Y:    rows * uint16(cellH),
	})
}

func (p *PTY) Fd() int {
	return int(p.ptmx.Fd())
}

func (p *PTY) Close() {
	// Signal the process to exit, then close the master pty fd.
	if p.cmd.Process != nil {
		p.cmd.Process.Signal(syscall.SIGHUP)
	}
	p.ptmx.Close()

	go func() {
		p.cmd.Wait()
		// Clean up sandbox resources
		if p.containerID != "" {
			exec.Command("runsc", "--root="+p.stateDir, "kill", p.containerID, "KILL").Run()
			exec.Command("runsc", "--root="+p.stateDir, "delete", p.containerID).Run()
		}
		if p.bundleDir != "" {
			os.RemoveAll(p.bundleDir)
		}
		if p.stateDir != "" {
			os.RemoveAll(p.stateDir)
		}
		if p.pastaNetns != nil {
			p.pastaNetns.Stop()
		}
	}()
}

// recvFd receives a file descriptor over a Unix socket via SCM_RIGHTS.
func recvFd(conn *net.UnixConn) (*os.File, error) {
	buf := make([]byte, 1)
	oob := make([]byte, syscall.CmsgSpace(4))
	_, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, fmt.Errorf("ReadMsgUnix: %w", err)
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, fmt.Errorf("ParseSocketControlMessage: %w", err)
	}
	for _, msg := range msgs {
		fds, err := syscall.ParseUnixRights(&msg)
		if err != nil {
			continue
		}
		if len(fds) > 0 {
			return os.NewFile(uintptr(fds[0]), "console"), nil
		}
	}
	return nil, fmt.Errorf("no fd received")
}

func SpawnSandboxedPTY(shell string, cols, rows uint16, cellW, cellH int, extraEnv []string, allowedLANRanges []string, mountHome bool, extraMounts []sandbox.MountSpec) (*PTY, error) {
	if shell == "" {
		shell = defaultShell()
	}

	// Create temp dirs for bundle and state
	bundleDir, err := os.MkdirTemp("", "claudehouse-bundle-")
	if err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	stateDir, err := os.MkdirTemp("", "claudehouse-state-")
	if err != nil {
		os.RemoveAll(bundleDir)
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	cleanup := func() {
		os.RemoveAll(bundleDir)
		os.RemoveAll(stateDir)
	}

	// Start pasta first so we can discover the gateway IP for proxy env vars.
	var proxyAllowPort int
	for _, e := range extraEnv {
		if strings.HasPrefix(e, "CLAUDEHOUSE_PROXY_PORT=") {
			fmt.Sscanf(strings.TrimPrefix(e, "CLAUDEHOUSE_PROXY_PORT="), "%d", &proxyAllowPort)
		}
	}
	pastaNetns, err := sandbox.StartPasta(proxyAllowPort, allowedLANRanges)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("start pasta: %w", err)
	}

	// Rewrite proxy env vars to use the gateway IP (which pasta maps to the host).
	if pastaNetns.GatewayIP != "" && proxyAllowPort > 0 {
		proxyURL := fmt.Sprintf("http://%s:%d", pastaNetns.GatewayIP, proxyAllowPort)
		for i, e := range extraEnv {
			if strings.HasPrefix(e, "HTTPS_PROXY=") {
				extraEnv[i] = "HTTPS_PROXY=" + proxyURL
			} else if strings.HasPrefix(e, "HTTP_PROXY=") {
				extraEnv[i] = "HTTP_PROXY=" + proxyURL
			}
		}
	}

	// Generate OCI bundle (after pasta so env vars have the correct proxy host)
	if err := sandbox.GenerateBundle(bundleDir, shell, extraEnv, mountHome, extraMounts); err != nil {
		pastaNetns.Stop()
		cleanup()
		return nil, fmt.Errorf("generate bundle: %w", err)
	}

	containerID := filepath.Base(bundleDir)

	// Set up console socket — runsc sends the PTY master FD over this
	sockPath := filepath.Join(bundleDir, "console.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		pastaNetns.Stop()
		cleanup()
		return nil, fmt.Errorf("listen console socket: %w", err)
	}

	// Channel to receive the PTY master from the console socket
	type consoleResult struct {
		file *os.File
		err  error
	}
	consoleCh := make(chan consoleResult, 1)
	go func() {
		defer listener.Close()
		conn, err := listener.AcceptUnix()
		if err != nil {
			consoleCh <- consoleResult{err: fmt.Errorf("accept: %w", err)}
			return
		}
		defer conn.Close()
		f, err := recvFd(conn)
		consoleCh <- consoleResult{file: f, err: err}
	}()

	// Run runsc inside pasta's network namespace via nsenter
	cmd := exec.Command("nsenter",
		"--user="+pastaNetns.UserNsPath,
		"--net="+pastaNetns.NsPath,
		"--preserve-credentials",
		"--",
		"runsc",
		"--root="+stateDir,
		"--rootless",
		"--platform=systrap",
		"--network=host",
		"--ignore-cgroups",
		"run",
		"--bundle="+bundleDir,
		"--console-socket="+sockPath,
		containerID,
	)
	var runscStderr bytes.Buffer
	cmd.Stderr = &runscStderr

	if err := cmd.Start(); err != nil {
		listener.Close()
		pastaNetns.Stop()
		cleanup()
		return nil, fmt.Errorf("start runsc: %w", err)
	}

	// Wait for console PTY master from runsc (with timeout)
	var result consoleResult
	select {
	case result = <-consoleCh:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		pastaNetns.Stop()
		cleanup()
		return nil, fmt.Errorf("timeout waiting for console socket from runsc\nstderr: %s", runscStderr.String())
	}
	if result.err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		pastaNetns.Stop()
		cleanup()
		return nil, fmt.Errorf("receive console fd: %w", result.err)
	}

	ptmx := result.file

	// Set initial terminal size
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    cols * uint16(cellW),
		Y:    rows * uint16(cellH),
	})

	p := &PTY{
		ptmx:        ptmx,
		cmd:         cmd,
		output:      make(chan []byte, 256),
		done:        make(chan struct{}),
		bundleDir:   bundleDir,
		stateDir:    stateDir,
		containerID: containerID,
		pastaNetns:  pastaNetns,
	}

	go p.readLoop()
	return p, nil
}
