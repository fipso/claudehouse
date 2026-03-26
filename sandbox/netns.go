package sandbox

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
)

// PastaNetns holds state for a pasta-managed network namespace.
type PastaNetns struct {
	PastaCmd   *exec.Cmd
	SleepCmd   *exec.Cmd
	NsPath     string // /proc/<sleep-pid>/ns/net
	UserNsPath string // /proc/<sleep-pid>/ns/user
}

// StartPasta creates a network namespace via pasta with internet access
// but no direct access to local IPs. Returns the netns path for nsenter.
func StartPasta(bundleDir string) (*PastaNetns, error) {
	// Step 1: Create a long-lived process in a new user+network namespace.
	// --user --map-root-user: create user namespace (allows net namespace without root)
	// --net: create network namespace
	sleepCmd := exec.Command("unshare", "--user", "--map-root-user", "--net", "sleep", "infinity")
	if err := sleepCmd.Start(); err != nil {
		return nil, fmt.Errorf("start unshare: %w", err)
	}

	sleepPid := sleepCmd.Process.Pid
	nsPath := fmt.Sprintf("/proc/%d/ns/net", sleepPid)
	userNsPath := fmt.Sprintf("/proc/%d/ns/user", sleepPid)

	// Step 2: Point pasta at this PID to set up networking in its netns.
	// pasta will configure the interface and provide NAT.
	pastaCmd := exec.Command("pasta",
		"--config-net",
		"--no-map-gw",
		"--ns-ifname", "eth0",
		"-d",
		fmt.Sprintf("%d", sleepPid),
	)
	var pastaStderr bytes.Buffer
	pastaCmd.Stderr = &pastaStderr

	if err := pastaCmd.Start(); err != nil {
		sleepCmd.Process.Kill()
		sleepCmd.Wait()
		return nil, fmt.Errorf("start pasta: %w", err)
	}

	// Wait for pasta to finish configuring (it daemonizes and exits quickly in PID mode)
	done := make(chan error, 1)
	go func() { done <- pastaCmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			sleepCmd.Process.Kill()
			sleepCmd.Wait()
			return nil, fmt.Errorf("pasta setup failed: %w\nstderr: %s", err, pastaStderr.String())
		}
	case <-time.After(10 * time.Second):
		pastaCmd.Process.Kill()
		sleepCmd.Process.Kill()
		sleepCmd.Wait()
		return nil, fmt.Errorf("timeout waiting for pasta setup")
	}

	return &PastaNetns{
		PastaCmd: pastaCmd,
		SleepCmd:   sleepCmd,
		NsPath:     nsPath,
		UserNsPath: userNsPath,
	}, nil
}

// Stop kills the processes and cleans up the namespace.
func (p *PastaNetns) Stop() {
	if p.SleepCmd != nil && p.SleepCmd.Process != nil {
		p.SleepCmd.Process.Kill()
		p.SleepCmd.Wait()
	}
}
