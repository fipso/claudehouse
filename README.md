# claudehouse

An infinite canvas terminal multiplexer. Spawn, arrange, and resize terminal windows freely on a 2D canvas with smooth zoom and pan.

Built with [Raylib](https://www.raylib.com/) + [Ghostty](https://ghostty.org/) terminal emulation.

![usage example](demo.gif)

## Sandbox

Terminals can optionally run inside a [gVisor](https://gvisor.dev/) sandbox for syscall-level isolation. Toggle sandbox mode with `Alt+S` — new terminals will be created inside a gVisor container with:

- Kernel-level syscall interception via gVisor's `runsc`
- Read-only host filesystem (home directory writable)
- Network isolation via [pasta](https://passt.top/) (internet access, local IPs blocked)
- Orange border on sandboxed terminals to distinguish them

Requires `runsc` and `pasta` on `PATH`.

## Keybinds

### Canvas (no terminal focused)

| Key | Action |
|---|---|
| Double-click | Create new terminal |
| Click | Focus terminal |
| Alt+Click | Move terminal |
| Right-click drag | Select multiple terminals |
| Scroll | Zoom in/out |
| Arrow keys | Pan camera |
| Alt+HJKL | Focus nearest terminal |
| Alt+Plus/Minus | Zoom in/out (large steps) |
| Alt+S | Toggle sandbox mode |

### Terminal (focused)

| Key | Action |
|---|---|
| 2x Esc | Unfocus terminal |
| Alt+HJKL | Navigate to adjacent terminal |
| Alt+E | Zoom camera to fit terminal |
| Alt+F | Resize terminal to fill viewport |
| Alt+Enter | Spawn new terminal to the right (same size) |
| Alt+Q | Close terminal |
| Alt+Plus/Minus | Zoom in/out |
| Alt+S | Toggle sandbox mode |
| Drag edge | Resize terminal |
