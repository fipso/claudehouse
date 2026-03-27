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
		// Bind-mount an empty dir over home to mask it from the read-only root.
		// We use a real directory (not tmpfs) so sub-mount destinations can be
		// created inside it by the runtime.
		emptyHome := filepath.Join(rootfs, "empty-home")
		os.MkdirAll(emptyHome, 0755)

		// Always mount the claudehouse config dir so the MITM proxy CA cert is accessible.
		chConfigDir := filepath.Join(home, ".config", "claudehouse")
		extraMounts = append(extraMounts, MountSpec{Path: chConfigDir, Mode: "ro"})

		// Pre-create mount points inside the empty home so runsc can mount over them.
		for _, m := range extraMounts {
			if rel, err := filepath.Rel(home, m.Path); err == nil && !filepath.IsAbs(rel) {
				dest := filepath.Join(emptyHome, rel)
				// Resolve symlinks to check if source is a file or directory.
				source := m.Path
				if resolved, err := filepath.EvalSymlinks(source); err == nil {
					source = resolved
				}
				if info, err := os.Stat(source); err == nil && !info.IsDir() {
					// File mount: create parent dir and empty file.
					os.MkdirAll(filepath.Dir(dest), 0755)
					os.WriteFile(dest, nil, 0644)
				} else {
					os.MkdirAll(dest, 0755)
				}
			}
		}
		mounts = append(mounts, map[string]any{
			"destination": home,
			"source":      emptyHome,
			"type":        "bind",
			"options":     []string{"rbind", "rw"},
		})
	}

	for _, m := range extraMounts {
		opts := []string{"rbind", "ro"}
		if m.Mode == "rw" {
			opts = []string{"rbind", "rw"}
		}
		// Resolve symlinks so bind mounts work when the path is a symlink.
		source := m.Path
		if resolved, err := filepath.EvalSymlinks(source); err == nil {
			source = resolved
		}
		mounts = append(mounts, map[string]any{
			"destination": m.Path,
			"source":      source,
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
