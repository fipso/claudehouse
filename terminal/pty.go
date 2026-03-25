package terminal

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"syscall"

	"github.com/creack/pty"
)

type PTY struct {
	ptmx   *os.File
	cmd    *exec.Cmd
	output chan []byte
	done   chan struct{}
}

func SpawnPTY(shell string, cols, rows uint16, cellW, cellH int) (*PTY, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		if u, err := user.Current(); err == nil {
			// Try to get shell from /etc/passwd via user info
			// user.Current() doesn't expose shell directly on all platforms
			_ = u
		}
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

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
	// Wait in background so we never block the render thread.
	go p.cmd.Wait()
}
