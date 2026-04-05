package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"claudebox"

	"github.com/creack/pty"
)

type PTY struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	output     chan []byte
	done       chan struct{}
	sandboxPTY *claudebox.SandboxedPTY // non-nil for sandboxed PTYs
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

func SpawnSandboxedPTY(shell string, cols, rows uint16, cellW, cellH int, extraEnv []string, allowedLANRanges []string, mountHome bool, extraMounts []claudebox.MountSpec) (*PTY, error) {
	// Extract proxy port from CLAUDEHOUSE_PROXY_PORT env var (set by makeProxyEnv).
	var proxyAllowPort int
	var filteredEnv []string
	for _, e := range extraEnv {
		if strings.HasPrefix(e, "CLAUDEHOUSE_PROXY_PORT=") {
			fmt.Sscanf(strings.TrimPrefix(e, "CLAUDEHOUSE_PROXY_PORT="), "%d", &proxyAllowPort)
		} else {
			filteredEnv = append(filteredEnv, e)
		}
	}
	extraEnv = filteredEnv

	// Always mount the claudehouse config dir so the MITM proxy CA cert is accessible.
	if !mountHome {
		homeDir, _ := os.UserHomeDir()
		chConfigDir := filepath.Join(homeDir, ".config", "claudehouse")
		extraMounts = append(extraMounts, claudebox.MountSpec{Path: chConfigDir, Mode: "ro"})
	}

	spty, err := claudebox.SpawnSandboxedPTY(shell, cols, rows, cellW, cellH, extraEnv, allowedLANRanges, proxyAllowPort, mountHome, extraMounts)
	if err != nil {
		return nil, err
	}

	return &PTY{
		ptmx:       nil, // managed by sandboxPTY
		sandboxPTY: spty,
		output:     make(chan []byte, 256),
		done:       make(chan struct{}),
	}, nil
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
	if p.sandboxPTY != nil {
		return p.sandboxPTY.Output()
	}
	return p.output
}

func (p *PTY) Done() <-chan struct{} {
	if p.sandboxPTY != nil {
		return p.sandboxPTY.Done()
	}
	return p.done
}

func (p *PTY) Write(data []byte) {
	if p.sandboxPTY != nil {
		p.sandboxPTY.Write(data)
		return
	}
	_, _ = p.ptmx.Write(data)
}

func (p *PTY) Resize(cols, rows uint16, cellW, cellH int) {
	if p.sandboxPTY != nil {
		p.sandboxPTY.Resize(cols, rows, cellW, cellH)
		return
	}
	pty.Setsize(p.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    cols * uint16(cellW),
		Y:    rows * uint16(cellH),
	})
}

func (p *PTY) Fd() int {
	if p.sandboxPTY != nil {
		return p.sandboxPTY.Fd()
	}
	return int(p.ptmx.Fd())
}

func (p *PTY) Close() {
	if p.sandboxPTY != nil {
		p.sandboxPTY.Close()
		return
	}
	// Signal the process to exit, then close the master pty fd.
	if p.cmd.Process != nil {
		p.cmd.Process.Signal(syscall.SIGHUP)
	}
	p.ptmx.Close()

	go func() {
		p.cmd.Wait()
	}()
}
