package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Font                  string                    `toml:"font"`
	GridSize              float64                   `toml:"grid_size"`
	SnapToGrid            bool                      `toml:"snap_to_grid"`
	ZoomToFitGap          int                       `toml:"zoom_to_fit_gap"`
	FillViewportGap       int                       `toml:"fill_viewport_gap"`
	DefaultSandboxProfile string                    `toml:"default_sandbox_profile"`
	Sandbox               map[string]SandboxProfile `toml:"sandbox"`
}

type SandboxProfile struct {
	AllowedLANRanges []string     `toml:"allowed_lan_ranges"`
	MountHome        *bool        `toml:"mount_home"` // nil = true (default)
	Mounts           []MountEntry `toml:"mounts"`
}

// ShouldMountHome returns whether the home directory should be auto-mounted rw.
func (p SandboxProfile) ShouldMountHome() bool {
	return p.MountHome == nil || *p.MountHome
}

type MountEntry struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"` // "ro" or "rw"
}

func DefaultConfig() *Config {
	return &Config{
		Font:                  "",
		GridSize:              50.0,
		SnapToGrid:            true,
		ZoomToFitGap:          100,
		FillViewportGap:       40,
		DefaultSandboxProfile: "default",
		Sandbox: map[string]SandboxProfile{
			"default": {},
		},
	}
}

const exampleConfig = `# Path to a TTF font file. Leave empty to use the built-in JetBrains Mono Nerd Font.
# font = "/path/to/font.ttf"

# Canvas grid point distance in pixels.
grid_size = 50.0

# Snap terminals to the grid when placed or moved.
snap_to_grid = true

# Pixel gap around the terminal when using Alt+E (zoom to fit).
zoom_to_fit_gap = 100

# Pixel gap around the terminal when using Alt+F (fill viewport).
fill_viewport_gap = 40

# Which sandbox profile to use for new sandboxed terminals.
default_sandbox_profile = "default"

# Sandbox profiles define network and filesystem rules for sandboxed terminals.
# The "default" profile blocks all LAN access and mounts $HOME rw.
[sandbox.default]
# mount_home = true   # Set to false to hide your home directory (tmpfs overlay).
# allowed_lan_ranges = ["192.168.1.0/24"]

# Add extra bind mounts with [[sandbox.default.mounts]]:
# [[sandbox.default.mounts]]
# path = "/data/projects"
# mode = "ro"
#
# [[sandbox.default.mounts]]
# path = "/mnt/shared"
# mode = "rw"
`

// Load reads config.toml from configDir. If the file does not exist, it writes
// an example config and returns defaults.
func Load(configDir string) (*Config, error) {
	cfg := DefaultConfig()
	path := filepath.Join(configDir, "config.toml")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Write example config for the user to discover.
			if mkErr := os.MkdirAll(configDir, 0755); mkErr == nil {
				os.WriteFile(path, []byte(exampleConfig), 0644)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// ActiveSandboxProfile returns the sandbox profile named by DefaultSandboxProfile,
// or an empty profile if not found.
func (c *Config) ActiveSandboxProfile() SandboxProfile {
	if p, ok := c.Sandbox[c.DefaultSandboxProfile]; ok {
		return p
	}
	return SandboxProfile{}
}

func (c *Config) validate() error {
	if c.Font != "" {
		if _, err := os.Stat(c.Font); err != nil {
			return fmt.Errorf("font %q: %w", c.Font, err)
		}
	}

	for name, profile := range c.Sandbox {
		for _, cidr := range profile.AllowedLANRanges {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("sandbox profile %q: invalid CIDR %q: %w", name, cidr, err)
			}
		}
		for _, m := range profile.Mounts {
			if m.Mode != "ro" && m.Mode != "rw" {
				return fmt.Errorf("sandbox profile %q: mount %q: mode must be \"ro\" or \"rw\", got %q", name, m.Path, m.Mode)
			}
		}
	}

	return nil
}
