package sandbox

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PastaNetns holds state for a pasta-managed network namespace.
type PastaNetns struct {
	PastaCmd   *exec.Cmd
	SleepCmd   *exec.Cmd
	NsPath     string // /proc/<sleep-pid>/ns/net
	UserNsPath string // /proc/<sleep-pid>/ns/user
	GatewayIP  string // default gateway inside namespace (maps to host)
}

// StartPasta creates a network namespace via pasta with internet access
// but no direct access to local IPs. Returns the netns path for nsenter.
// The gateway IP inside the namespace is mapped to the host by pasta,
// allowing the sandbox to reach the MITM proxy.
func StartPasta(proxyAllowPort int) (*PastaNetns, error) {
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

	// Step 3: Discover the default gateway inside the namespace.
	// Without --no-map-gw, pasta maps the gateway IP to the host, so the
	// sandbox can reach the proxy via this address.
	gwIP, err := namespaceGateway(userNsPath, nsPath)
	if err != nil {
		sleepCmd.Process.Kill()
		sleepCmd.Wait()
		return nil, fmt.Errorf("discover gateway: %w", err)
	}

	// Step 4: Block access to private/LAN IP ranges inside the namespace.
	// pasta provides full NAT by default — without these rules, the sandbox
	// can reach devices on the local network.
	// The proxy is whitelisted via gwIP:proxyAllowPort before the REJECT rules.
	if err := blockLANAccess(userNsPath, nsPath, gwIP, proxyAllowPort); err != nil {
		sleepCmd.Process.Kill()
		sleepCmd.Wait()
		return nil, fmt.Errorf("block LAN access: %w", err)
	}

	return &PastaNetns{
		PastaCmd:   pastaCmd,
		SleepCmd:   sleepCmd,
		NsPath:     nsPath,
		UserNsPath: userNsPath,
		GatewayIP:  gwIP,
	}, nil
}

// namespaceGateway returns the default gateway IP inside the given network namespace.
func namespaceGateway(userNsPath, nsPath string) (string, error) {
	out, err := exec.Command("nsenter",
		"--user="+userNsPath,
		"--net="+nsPath,
		"--preserve-credentials",
		"--",
		"ip", "route", "show", "default",
	).Output()
	if err != nil {
		return "", fmt.Errorf("ip route show default: %w", err)
	}
	// Output: "default via X.X.X.X dev eth0 ..."
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no gateway in: %s", string(out))
}

// blockLANAccess adds iptables/ip6tables rules inside the network namespace
// to reject all traffic to private (RFC 1918), link-local, and loopback ranges.
func blockLANAccess(userNsPath, nsPath string, proxyAllowHost string, proxyAllowPort int) error {
	// If a proxy is configured, allow traffic to it before blocking private ranges.
	if proxyAllowHost != "" && proxyAllowPort > 0 {
		cmd := exec.Command("nsenter",
			"--user="+userNsPath,
			"--net="+nsPath,
			"--preserve-credentials",
			"--",
			"iptables", "-A", "OUTPUT",
			"-d", proxyAllowHost,
			"-p", "tcp", "--dport", fmt.Sprintf("%d", proxyAllowPort),
			"-j", "ACCEPT",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("iptables allow proxy %s:%d: %v (%s)", proxyAllowHost, proxyAllowPort, err, string(out))
		}
	}

	// IPv4 private and special ranges
	ipv4Ranges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"127.0.0.0/8",
	}

	// IPv6 private and link-local ranges
	ipv6Ranges := []string{
		"fc00::/7",
		"fe80::/10",
		"::1/128",
	}

	var errs []string

	for _, cidr := range ipv4Ranges {
		cmd := exec.Command("nsenter",
			"--user="+userNsPath,
			"--net="+nsPath,
			"--preserve-credentials",
			"--",
			"iptables", "-A", "OUTPUT", "-d", cidr, "-j", "REJECT",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("iptables block %s: %v (%s)", cidr, err, string(out)))
		}
	}

	for _, cidr := range ipv6Ranges {
		cmd := exec.Command("nsenter",
			"--user="+userNsPath,
			"--net="+nsPath,
			"--preserve-credentials",
			"--",
			"ip6tables", "-A", "OUTPUT", "-d", cidr, "-j", "REJECT",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("ip6tables block %s: %v (%s)", cidr, err, string(out)))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to add firewall rules:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

// Stop kills the processes and cleans up the namespace.
func (p *PastaNetns) Stop() {
	if p.SleepCmd != nil && p.SleepCmd.Process != nil {
		p.SleepCmd.Process.Kill()
		p.SleepCmd.Wait()
	}
}
