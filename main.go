package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"claudehouse/agent"
	"claudehouse/canvas"
	"claudehouse/config"
	"claudehouse/proxy"
	"claudehouse/sandbox"
	"claudehouse/terminal"

	rl "github.com/gen2brain/raylib-go/raylib"
)

//go:embed fonts/JetBrainsMonoNerdFont-Regular.ttf
var fontFS embed.FS

func fontCodepoints() []rune {
	ranges := [][2]rune{
		{0x0020, 0x007E}, // ASCII
		{0x00A0, 0x00FF}, // Latin-1 Supplement
		{0x0100, 0x017F}, // Latin Extended-A
		{0x2000, 0x206F}, // General Punctuation
		{0x2190, 0x21FF}, // Arrows
		{0x2200, 0x22FF}, // Mathematical Operators
		{0x2300, 0x23FF}, // Miscellaneous Technical
		{0x2500, 0x257F}, // Box Drawing
		{0x2580, 0x259F}, // Block Elements
		{0x25A0, 0x25FF}, // Geometric Shapes
		{0x2600, 0x26FF}, // Miscellaneous Symbols
		{0x2700, 0x27BF}, // Dingbats
		{0x2800, 0x28FF}, // Braille Patterns
		{0xE000, 0xE0FF}, // Private Use Area (Powerline/Nerd)
		{0xE200, 0xE2FF}, // Nerd Fonts - Seti-UI + Custom
		{0xE700, 0xE7FF}, // Nerd Fonts - Devicons
		{0xF000, 0xF2FF}, // Nerd Fonts - Font Awesome
		{0xF300, 0xF3FF}, // Nerd Fonts - Font Awesome Extension
		{0xF400, 0xF4FF}, // Nerd Fonts - Octicons
		{0xF500, 0xF5FF}, // Nerd Fonts - Font Logos
		{0xF600, 0xF6FF}, // Nerd Fonts - Codicons
		{0xFFFD, 0xFFFD}, // Replacement character
	}
	var cp []rune
	for _, r := range ranges {
		for c := r[0]; c <= r[1]; c++ {
			cp = append(cp, c)
		}
	}
	return cp
}

func main() {
	runtime.LockOSThread()

	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(1280, 800, "claudehouse")
	rl.SetTargetFPS(60)
	rl.SetExitKey(0) // Don't close on Escape
	defer rl.CloseWindow()

	// Load configuration
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "claudehouse")
	cfg, err := config.Load(configDir)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Apply canvas config
	canvas.GridSize = float32(cfg.GridSize)
	canvas.SnapEnabled = cfg.SnapToGrid
	canvas.ZoomToFitGap = cfg.ZoomToFitGap
	canvas.FillViewportGap = cfg.FillViewportGap

	// Load font — from config path or embedded default
	var fontData []byte
	if cfg.Font != "" {
		fontData, err = os.ReadFile(cfg.Font)
		if err != nil {
			log.Printf("WARNING: failed to load font %q, using default: %v", cfg.Font, err)
			fontData = nil
		}
	}
	if fontData == nil {
		fontData, err = fontFS.ReadFile("fonts/JetBrainsMonoNerdFont-Regular.ttf")
		if err != nil {
			panic("failed to load embedded font: " + err.Error())
		}
	}

	dpiScale := rl.GetWindowScaleDPI()
	fontSize := int32(16)
	fontSizePx := int32(float32(fontSize) * dpiScale.Y)
	fontLoadSize := fontSizePx * 3 // Load at 3x for crisp zoom
	font := rl.LoadFontFromMemory(".ttf", fontData, fontLoadSize, fontCodepoints())
	rl.SetTextureFilter(font.Texture, rl.FilterBilinear)
	defer rl.UnloadFont(font)

	// Measure cell size at physical font size (BeginMode2D operates in framebuffer pixels)
	glyphSize := rl.MeasureTextEx(font, "M", float32(fontSizePx), 0)
	cellW := int(glyphSize.X)
	cellH := int(glyphSize.Y)
	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}

	// Initialize MITM proxy for intercepting Claude Code API traffic
	mitmProxy, err := proxy.NewProxy(configDir)
	if err != nil {
		log.Printf("WARNING: failed to start MITM proxy: %v", err)
	}

	// Helper to create proxy env vars for a terminal.
	termCounter := 0
	makeProxyEnv := func(sandboxed bool) []string {
		if mitmProxy == nil {
			return nil
		}
		bindHost := "127.0.0.1"
		if sandboxed {
			bindHost = "0.0.0.0"
		}
		termCounter++
		termID := fmt.Sprintf("term-%d", termCounter)
		tp, err := mitmProxy.StartTerminalProxy(termID, bindHost)
		if err != nil {
			log.Printf("WARNING: failed to start terminal proxy: %v", err)
			return nil
		}
		proxyURL := fmt.Sprintf("http://127.0.0.1:%d", tp.Port)
		env := []string{
			"HTTPS_PROXY=" + proxyURL,
			"HTTP_PROXY=" + proxyURL,
			"NODE_EXTRA_CA_CERTS=" + mitmProxy.CA.CertPath,
		}
		if sandboxed {
			// HTTPS_PROXY/HTTP_PROXY will be rewritten by SpawnSandboxedPTY
			// to use the pasta gateway IP (which maps to the host).
			env = append(env, fmt.Sprintf("CLAUDEHOUSE_PROXY_PORT=%d", tp.Port))
		}
		return env
	}
	// Avoid unused variable warning when proxy is nil
	_ = makeProxyEnv

	// Create canvas
	c := canvas.NewCanvas()
	defer c.FreeAll()

	// Convert active sandbox profile mounts to sandbox.MountSpec.
	sandboxProfile := cfg.ActiveSandboxProfile()
	var extraMounts []sandbox.MountSpec
	for _, m := range sandboxProfile.Mounts {
		path, mode, _ := config.ParseMount(m) // already validated
		extraMounts = append(extraMounts, sandbox.MountSpec{Path: path, Mode: mode})
	}

	// Set up the node creation function
	canvas.CreateNodeFunc = func(pos rl.Vector2) canvas.Node {
		proxyEnv := makeProxyEnv(canvas.SandboxMode)
		tn, err := terminal.NewTerminalNode(pos, 80, 24, font, int(fontSizePx), cellW, cellH, "", canvas.SandboxMode, proxyEnv, sandboxProfile.AllowedLANRanges, sandboxProfile.ShouldMountHome(), extraMounts)
		if err != nil {
			rl.TraceLog(rl.LogError, "Failed to create terminal: %s", err.Error())
			return nil
		}
		return tn
	}

	// Create initial terminal
	proxyEnv := makeProxyEnv(false)
	term, err := terminal.NewTerminalNode(
		rl.Vector2{X: 50, Y: 50}, 80, 24, font, int(fontSizePx), cellW, cellH, "", false, proxyEnv, nil, true, nil)
	if err != nil {
		panic("failed to create terminal: " + err.Error())
	}
	c.AddNode(term)
	c.FocusedIdx = 0
	term.SetFocused(true)

	// Track agent nodes: by stream ID for event routing, by terminal ID for layout.
	agentByStream := map[string]*agent.AgentNode{}
	agentsByTerminal := map[string][]*agent.AgentNode{}
	// Parent-child inference: when a stream uses the Agent tool, the next new stream is its child.
	pendingParent := map[string]string{} // terminalID → streamID of stream that called Agent tool
	agentCounter := map[string]int{}     // terminalID → next agent number

	// findTerminalNode finds the terminal node matching a terminal ID.
	// Terminal IDs are "term-N" and terminals are created in order.
	findTerminalNode := func(terminalID string) (rl.Vector2, rl.Vector2, bool) {
		// Parse the terminal index from "term-N"
		var idx int
		if _, err := fmt.Sscanf(terminalID, "term-%d", &idx); err != nil {
			return rl.Vector2{}, rl.Vector2{}, false
		}
		// Find the idx-th non-agent node (terminal nodes are created first)
		termIdx := 0
		for _, node := range c.Nodes {
			if _, isAgent := node.(*agent.AgentNode); isAgent {
				continue
			}
			termIdx++
			if termIdx == idx {
				return node.Position(), node.Size(), true
			}
		}
		return rl.Vector2{}, rl.Vector2{}, false
	}

	for !rl.WindowShouldClose() {
		c.HandleInput()

		// Drain proxy events and create/update agent nodes.
		if mitmProxy != nil {
			for {
				select {
				case evt := <-mitmProxy.Registry.Events:
					switch evt.Type {
					case "stream_start":
						// Find parent terminal position and size.
						parentPos, parentSize, found := findTerminalNode(evt.TerminalID)
						if !found {
							if len(c.Nodes) > 0 {
								parentPos = c.Nodes[0].Position()
								parentSize = c.Nodes[0].Size()
							}
						}

						// Count active (non-closing) agent nodes for this terminal to determine X offset.
						existing := agentsByTerminal[evt.TerminalID]
						activeCount := 0
						for _, an := range existing {
							if !an.Closing() {
								activeCount++
							}
						}

						// Place below terminal, tiled rightward.
						agentWidth := float32(agent.NodeCols*cellW + 2*agent.NodePad)
						gap := float32(20)
						agentPos := rl.Vector2{
							X: parentPos.X + float32(activeCount)*(agentWidth+gap),
							Y: parentPos.Y + parentSize.Y + gap,
						}

						// Assign sequential number.
						agentCounter[evt.TerminalID]++
						num := agentCounter[evt.TerminalID]

						an := agent.NewAgentNode(agentPos, font, int(fontSizePx), cellW, cellH,
							evt.StreamID, evt.TerminalID, evt.IsSubagent)
						an.Number = num
						an.Model = evt.Content // stream_start content is the model name

						// Check if there's a pending parent (a stream that called the Agent tool).
						if parentStreamID, ok := pendingParent[evt.TerminalID]; ok {
							an.ParentStreamID = parentStreamID
							if parentNode, ok := agentByStream[parentStreamID]; ok {
								an.ParentLabel = fmt.Sprintf("#%d", parentNode.Number)
							}
							delete(pendingParent, evt.TerminalID)
						}

						agentByStream[evt.StreamID] = an
						agentsByTerminal[evt.TerminalID] = append(agentsByTerminal[evt.TerminalID], an)
						c.AddNode(an)

					case "tool_start":
						// When a stream calls the Agent tool, mark it as pending parent.
						if evt.ToolName == "Agent" {
							pendingParent[evt.TerminalID] = evt.StreamID
						}
						if an, ok := agentByStream[evt.StreamID]; ok {
							select {
							case an.Events() <- evt:
							default:
							}
						}

					default:
						if an, ok := agentByStream[evt.StreamID]; ok {
							select {
							case an.Events() <- evt:
							default:
							}
						}
					}
				default:
					goto eventsDone
				}
			}
		eventsDone:
		}

		// Clean up despawned agent nodes and reflow positions.
		agentWidth := float32(agent.NodeCols*cellW + 2*agent.NodePad)
		gap := float32(20)
		for termID, nodes := range agentsByTerminal {
			alive := nodes[:0]
			for _, an := range nodes {
				if an.AnimDone() {
					delete(agentByStream, an.StreamID)
				} else {
					alive = append(alive, an)
				}
			}
			if len(alive) == 0 {
				delete(agentsByTerminal, termID)
				continue
			}
			agentsByTerminal[termID] = alive

			// Reflow: reposition all non-closing agents left-to-right.
			parentPos, parentSize, found := findTerminalNode(termID)
			if !found {
				continue
			}
			slot := 0
			for _, an := range alive {
				if an.Closing() {
					continue
				}
				targetX := parentPos.X + float32(slot)*(agentWidth+gap)
				targetY := parentPos.Y + parentSize.Y + gap
				pos := an.Position()
				// Smooth slide toward target position.
				lerpSpeed := float32(10.0 * rl.GetFrameTime())
				if lerpSpeed > 1.0 {
					lerpSpeed = 1.0
				}
				pos.X += (targetX - pos.X) * lerpSpeed
				pos.Y += (targetY - pos.Y) * lerpSpeed
				an.SetPosition(pos)
				slot++
			}
		}

		c.Update()

		rl.BeginDrawing()
		// Wayland compositors may provide a 2x framebuffer that raylib
		// doesn't detect. Override the viewport to the actual physical size.
		dpi := rl.GetWindowScaleDPI()
		rl.Viewport(0, 0,
			int32(float32(rl.GetScreenWidth())*dpi.X),
			int32(float32(rl.GetScreenHeight())*dpi.Y))
		rl.ClearBackground(rl.Color{R: 30, G: 30, B: 30, A: 255})
		c.Draw()
		rl.EndDrawing()
	}
}
