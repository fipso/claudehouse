package main

import (
	"embed"
	"runtime"

	"claudehouse/canvas"
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

	// Load embedded font at native DPI
	fontData, err := fontFS.ReadFile("fonts/JetBrainsMonoNerdFont-Regular.ttf")
	if err != nil {
		panic("failed to load embedded font: " + err.Error())
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

	// Create canvas
	c := canvas.NewCanvas()
	defer c.FreeAll()

	// Set up the node creation function
	canvas.CreateNodeFunc = func(pos rl.Vector2) canvas.Node {
		tn, err := terminal.NewTerminalNode(pos, 80, 24, font, int(fontSizePx), cellW, cellH, "", canvas.SandboxMode)
		if err != nil {
			rl.TraceLog(rl.LogError, "Failed to create terminal: %s", err.Error())
			return nil
		}
		return tn
	}

	// Create initial terminal
	term, err := terminal.NewTerminalNode(
		rl.Vector2{X: 50, Y: 50}, 80, 24, font, int(fontSizePx), cellW, cellH, "", false)
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
