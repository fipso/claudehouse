package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// MountSpec describes an extra bind mount for the sandbox.
type MountSpec struct {
	Path string
	Mode string // "ro" or "rw"
}

// GenerateBundle creates an OCI bundle directory with config.json for runsc.
// The rootfs is an empty directory — host / is bind-mounted read-only via the spec.
func GenerateBundle(dir, shell string, extraEnv []string, mountHome bool, extraMounts []MountSpec) error {
	rootfs := filepath.Join(dir, "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		return fmt.Errorf("create rootfs dir: %w", err)
	}

	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	home := u.HomeDir
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}

	mounts := []map[string]any{
		{
			"destination": "/",
			"source":      "/",
			"type":        "bind",
			"options":     []string{"rbind", "ro"},
		},
		{
			"destination": "/proc",
			"source":      "proc",
			"type":        "proc",
		},
		{
			"destination": "/tmp",
			"source":      "tmpfs",
			"type":        "tmpfs",
		},
		{
			"destination": "/dev",
			"source":      "tmpfs",
			"type":        "tmpfs",
		},
		{
			"destination": "/dev/pts",
			"source":      "devpts",
			"type":        "devpts",
		},
	}

	if mountHome {
		mounts = append(mounts, map[string]any{
			"destination": home,
			"source":      home,
			"type":        "bind",
			"options":     []string{"rbind", "rw"},
		})
	} else {
		// Mount tmpfs over home to mask it from the read-only root bind.
		mounts = append(mounts, map[string]any{
			"destination": home,
			"source":      "tmpfs",
			"type":        "tmpfs",
		})
	}

	for _, m := range extraMounts {
		opts := []string{"rbind", "ro"}
		if m.Mode == "rw" {
			opts = []string{"rbind", "rw"}
		}
		mounts = append(mounts, map[string]any{
			"destination": m.Path,
			"source":      m.Path,
			"type":        "bind",
			"options":     opts,
		})
	}

	spec := map[string]any{
		"ociVersion": "1.0.0",
		"process": map[string]any{
			"terminal": true,
			"user": map[string]any{
				"uid": 0,
				"gid": 0,
			},
			"args": []string{shell, "-l"},
			"env": append([]string{
				"TERM=xterm-256color",
				"HOME=" + home,
				"PATH=" + path,
				"USER=" + u.Username,
				"SHELL=" + shell,
			}, extraEnv...),
			"cwd": home,
		},
		"root": map[string]any{
			"path":     "rootfs",
			"readonly": false,
		},
		"mounts": mounts,
		"linux": map[string]any{
			"namespaces": []map[string]any{
				{"type": "pid"},
				{"type": "mount"},
				{"type": "uts"},
				{"type": "ipc"},
			},
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal OCI spec: %w", err)
	}

	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write config.json: %w", err)
	}

	return nil
}
