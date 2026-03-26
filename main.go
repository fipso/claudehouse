package main

import (
	"embed"
	"runtime"

	"claudehouse/canvas"
	"claudehouse/terminal"

	rl "github.com/gen2brain/raylib-go/raylib"
)

//go:embed fonts/JetBrainsMono-Regular.ttf
var fontFS embed.FS

func main() {
	runtime.LockOSThread()

	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(1280, 800, "claudehouse")
	rl.SetTargetFPS(60)
	rl.SetExitKey(0) // Don't close on Escape
	defer rl.CloseWindow()

	// Load embedded font at native DPI
	fontData, err := fontFS.ReadFile("fonts/JetBrainsMono-Regular.ttf")
	if err != nil {
		panic("failed to load embedded font: " + err.Error())
	}

	dpiScale := rl.GetWindowScaleDPI()
	fontSize := int32(16)
	fontSizePx := int32(float32(fontSize) * dpiScale.Y)
	fontLoadSize := fontSizePx * 3 // Load at 3x for crisp zoom
	font := rl.LoadFontFromMemory(".ttf", fontData, fontLoadSize, nil)
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

	// Create canvas
	c := canvas.NewCanvas()
	defer c.FreeAll()

	// Set up the node creation function for right-click
	canvas.CreateNodeFunc = func(pos rl.Vector2) canvas.Node {
		tn, err := terminal.NewTerminalNode(pos, 80, 24, font, int(fontSizePx), cellW, cellH, "")
		if err != nil {
			rl.TraceLog(rl.LogError, "Failed to create terminal: %s", err.Error())
			return nil
		}
		return tn
	}

	// Create initial terminal
	term, err := terminal.NewTerminalNode(
		rl.Vector2{X: 50, Y: 50}, 80, 24, font, int(fontSizePx), cellW, cellH, "")
	if err != nil {
		panic("failed to create terminal: " + err.Error())
	}
	c.AddNode(term)
	c.FocusedIdx = 0
	term.SetFocused(true)

	for !rl.WindowShouldClose() {
		c.HandleInput()
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
