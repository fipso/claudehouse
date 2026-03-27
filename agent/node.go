package agent

import (
	"fmt"
	"strings"
	"sync"

	"claudehouse/canvas"
	"claudehouse/proxy"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	NodePad  = 8
	NodeCols = 60
)

// ContentBlock represents a block of content in the agent output.
type ContentBlock struct {
	Type     string // "thinking", "text", "tool_use"
	Content  strings.Builder
	ToolName string
	Complete bool
}

// AgentNode displays structured Claude API output on the canvas.
type AgentNode struct {
	canvas.NodeBase

	mu sync.Mutex

	StreamID       string
	TerminalID     string
	IsSubagent     bool
	ParentStreamID string
	Number         int    // sequential agent number within terminal
	ParentLabel    string // e.g. "#1" if parent is agent #1
	Model          string // model name from stream_start

	blocks []*ContentBlock

	// Scroll
	scrollOffset int
	maxRows      int

	// RenderTexture caching
	texture       rl.RenderTexture2D
	mip2Texture   rl.RenderTexture2D // 2x intermediate mip
	mipTexture    rl.RenderTexture2D // 1x resolution for zoom-out
	texValid      bool
	dirty         bool
	contentHeight float32 // visible content height in world units (set by PreDraw)

	// Event channel for receiving updates
	events   chan proxy.AgentEvent
	done     bool    // stream finished
	doneTime float64 // time when stream_end was received
}

// NewAgentNode creates an agent output node.
func NewAgentNode(pos rl.Vector2, font rl.Font, fontSize, cellW, cellH int, streamID, terminalID string, isSubagent bool) *AgentNode {
	maxRows := 30
	s := int32(canvas.TexScale)
	logW := int32(NodeCols*cellW + 2*NodePad)
	logH := int32(maxRows*cellH + 2*NodePad)
	tex := rl.LoadRenderTexture(logW*s, logH*s)
	rl.SetTextureFilter(tex.Texture, rl.FilterBilinear)
	mip2 := rl.LoadRenderTexture(logW*2, logH*2)
	rl.SetTextureFilter(mip2.Texture, rl.FilterBilinear)
	mip := rl.LoadRenderTexture(logW, logH)
	rl.SetTextureFilter(mip.Texture, rl.FilterBilinear)

	return &AgentNode{
		NodeBase: canvas.NodeBase{
			Pos:      pos,
			Font:     font,
			FontSize: fontSize,
			CellW:    cellW,
			CellH:    cellH,
		},
		StreamID:   streamID,
		TerminalID: terminalID,
		IsSubagent: isSubagent,
		maxRows:    maxRows,
		events:     make(chan proxy.AgentEvent, 1024),
		texture:     tex,
		mip2Texture: mip2,
		mipTexture:  mip,
		texValid:   true,
		dirty:      true,
	}
}

func (n *AgentNode) Update() {
	n.TickAnim()

	// Auto-despawn after 3 seconds of being done
	if n.done && !n.IsClosing && n.doneTime > 0 {
		if float64(rl.GetTime())-n.doneTime > 3.0 {
			n.StartCloseAnim()
		}
	}

	// Drain events
	for {
		select {
		case evt := <-n.events:
			n.handleEvent(evt)
		default:
			return
		}
	}
}

func (n *AgentNode) handleEvent(evt proxy.AgentEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()

	switch evt.Type {
	case "thinking":
		if b := n.lastBlock("thinking"); b != nil {
			b.Content.WriteString(evt.Content)
		} else {
			cb := &ContentBlock{Type: "thinking"}
			cb.Content.WriteString(evt.Content)
			n.blocks = append(n.blocks, cb)
		}

	case "text":
		if b := n.lastBlock("text"); b != nil {
			b.Content.WriteString(evt.Content)
		} else {
			cb := &ContentBlock{Type: "text"}
			cb.Content.WriteString(evt.Content)
			n.blocks = append(n.blocks, cb)
		}

	case "tool_start":
		cb := &ContentBlock{Type: "tool_use", ToolName: evt.ToolName}
		n.blocks = append(n.blocks, cb)

	case "tool_delta":
		if b := n.lastBlock("tool_use"); b != nil {
			b.Content.WriteString(evt.Content)
		}

	case "tool_end":
		if b := n.lastBlock("tool_use"); b != nil {
			b.Complete = true
		}

	case "model":
		n.Model = evt.Content

	case "stream_end":
		n.done = true
		n.doneTime = float64(rl.GetTime())
	}

	n.dirty = true
}

func (n *AgentNode) lastBlock(typ string) *ContentBlock {
	for i := len(n.blocks) - 1; i >= 0; i-- {
		if n.blocks[i].Type == typ && !n.blocks[i].Complete {
			return n.blocks[i]
		}
	}
	return nil
}

func (n *AgentNode) PreDraw() {
	if !n.dirty || !n.texValid {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	s := int32(canvas.TexScale)
	sh := s * int32(n.CellH)
	sPad := s * int32(NodePad)
	sFontSize := float32(s) * float32(n.FontSize)

	var lines []renderedLine
	for _, b := range n.blocks {
		lines = append(lines, n.renderBlock(b)...)
	}

	headerText := fmt.Sprintf("Agent #%d", n.Number)
	if n.ParentLabel != "" {
		headerText = fmt.Sprintf("Agent #%d (parent: %s)", n.Number, n.ParentLabel)
	}
	if n.Model != "" {
		headerText += " " + n.Model
	}
	if n.done {
		headerText += " (done)"
	}

	totalRows := 1 + len(lines)
	if totalRows > n.maxRows {
		totalRows = n.maxRows
	}

	// Store content height in world units for Draw()
	n.contentHeight = float32(totalRows*n.CellH + 2*NodePad)

	// Determine header color for texture rendering
	headerColor := rl.Color{R: 160, G: 100, B: 255, A: 255}
	if n.IsSubagent {
		headerColor = rl.Color{R: 80, G: 200, B: 220, A: 255}
	}
	if n.done {
		headerColor.A = 128
	}

	rl.BeginTextureMode(n.texture)
	rl.ClearBackground(rl.Color{R: 25, G: 25, B: 35, A: 255})

	px := sPad
	py := sPad
	rl.DrawTextEx(n.Font, headerText, rl.Vector2{X: float32(px), Y: float32(py)},
		sFontSize, 0, headerColor)
	py += sh

	startLine := n.scrollOffset
	visibleRows := totalRows - 1
	if startLine > len(lines)-visibleRows {
		startLine = len(lines) - visibleRows
	}
	if startLine < 0 {
		startLine = 0
	}

	for i := startLine; i < len(lines) && (i-startLine) < visibleRows; i++ {
		line := lines[i]
		rl.DrawTextEx(n.Font, line.text, rl.Vector2{X: float32(px), Y: float32(py)},
			sFontSize, 0, line.color)
		py += sh
	}

	rl.EndTextureMode()

	// 2-step downsample: 4x→2x→1x
	sf := float32(s)
	logW := float32(NodeCols*n.CellW + 2*NodePad)
	logH := float32(n.maxRows*n.CellH + 2*NodePad)
	// 4x → 2x
	rl.BeginTextureMode(n.mip2Texture)
	rl.DrawTexturePro(n.texture.Texture,
		rl.Rectangle{X: 0, Y: 0, Width: logW * sf, Height: -logH * sf},
		rl.Rectangle{X: 0, Y: 0, Width: logW * 2, Height: logH * 2},
		rl.Vector2{}, 0, rl.White)
	rl.EndTextureMode()
	// 2x → 1x
	rl.BeginTextureMode(n.mipTexture)
	rl.DrawTexturePro(n.mip2Texture.Texture,
		rl.Rectangle{X: 0, Y: 0, Width: logW * 2, Height: -logH * 2},
		rl.Rectangle{X: 0, Y: 0, Width: logW, Height: logH},
		rl.Vector2{}, 0, rl.White)
	rl.EndTextureMode()

	n.dirty = false
}

func (n *AgentNode) Draw(camera rl.Camera2D) {
	_, offsetY, alpha := n.AnimOffset()

	width := float32(NodeCols*n.CellW + 2*NodePad)
	h := n.contentHeight
	if h <= 0 {
		h = float32(n.CellH + 2*NodePad) // minimum 1 row
	}
	maxH := float32(n.maxRows*n.CellH + 2*NodePad)

	drawX := n.Pos.X
	drawY := n.Pos.Y + offsetY

	// Pick mip level based on zoom: 4x, 2x, or 1x
	var tex rl.Texture2D
	var srcW, srcTexH, srcH float32
	if camera.Zoom >= 1.0 {
		s := float32(canvas.TexScale)
		tex = n.texture.Texture
		srcW = width * s
		srcTexH = maxH * s
		srcH = h * s
	} else if camera.Zoom >= 0.5 {
		tex = n.mip2Texture.Texture
		srcW = width * 2
		srcTexH = maxH * 2
		srcH = h * 2
	} else {
		tex = n.mipTexture.Texture
		srcW = width
		srcTexH = maxH
		srcH = h
	}
	// Content is at y=0 in render space → UV_y=1 in GL. Offset sourceRec.Y accordingly.
	sourceRec := rl.Rectangle{X: 0, Y: srcTexH - srcH, Width: srcW, Height: -srcH}
	destRec := rl.Rectangle{X: drawX, Y: drawY, Width: width, Height: h}
	rl.DrawTexturePro(tex, sourceRec, destRec,
		rl.Vector2{}, 0, rl.Color{R: 255, G: 255, B: 255, A: alpha})

	// Draw border directly (not in texture) for clean zoom
	borderColor := rl.Color{R: 160, G: 100, B: 255, A: alpha}
	if n.IsSubagent {
		borderColor = rl.Color{R: 80, G: 200, B: 220, A: alpha}
	}
	if n.done {
		borderColor.A = alpha / 2
	}
	rl.DrawRectangleLines(int32(drawX), int32(drawY), int32(width), int32(h), borderColor)
}

type renderedLine struct {
	text  string
	color rl.Color
}

func (n *AgentNode) renderBlock(b *ContentBlock) []renderedLine {
	var color rl.Color
	var prefix string

	switch b.Type {
	case "thinking":
		color = rl.Color{R: 120, G: 120, B: 140, A: 255} // gray
		prefix = ""
	case "text":
		color = rl.Color{R: 220, G: 220, B: 220, A: 255} // white
		prefix = ""
	case "tool_use":
		color = rl.Color{R: 255, G: 200, B: 80, A: 255} // yellow
		if b.ToolName != "" {
			prefix = "[" + b.ToolName + "] "
		}
	}

	text := prefix + b.Content.String()
	return wrapText(text, NodeCols, color)
}

func wrapText(text string, cols int, color rl.Color) []renderedLine {
	var lines []renderedLine
	for _, raw := range strings.Split(text, "\n") {
		if raw == "" {
			lines = append(lines, renderedLine{text: "", color: color})
			continue
		}
		for len(raw) > cols {
			lines = append(lines, renderedLine{text: raw[:cols], color: color})
			raw = raw[cols:]
		}
		lines = append(lines, renderedLine{text: raw, color: color})
	}
	return lines
}

// --- Node interface implementation ---

func (n *AgentNode) HandleKeyInput() {
	if n.IsFocused {
		if rl.IsKeyPressed(rl.KeyPageUp) {
			n.scrollOffset -= 10
			if n.scrollOffset < 0 {
				n.scrollOffset = 0
			}
			n.dirty = true
		}
		if rl.IsKeyPressed(rl.KeyPageDown) {
			n.scrollOffset += 10
			n.dirty = true
		}
	}
}

func (n *AgentNode) HandleMouseInput(camera rl.Camera2D, mouseWorld rl.Vector2) {
	if !n.Contains(mouseWorld) {
		return
	}
	wheel := rl.GetMouseWheelMove()
	if wheel != 0 {
		n.scrollOffset -= int(wheel * 3)
		if n.scrollOffset < 0 {
			n.scrollOffset = 0
		}
		n.dirty = true
	}
}

func (n *AgentNode) Size() rl.Vector2 {
	width := float32(NodeCols*n.CellW + 2*NodePad)
	h := n.contentHeight
	if h <= 0 {
		h = float32(n.CellH + 2*NodePad)
	}
	return rl.Vector2{X: width, Y: h}
}

func (n *AgentNode) SetSize(cols, rows uint16) {
	newMaxRows := int(rows)
	if newMaxRows != n.maxRows {
		n.maxRows = newMaxRows
		// Recreate texture for new size
		if n.texValid {
			rl.UnloadRenderTexture(n.texture)
			rl.UnloadRenderTexture(n.mip2Texture)
			rl.UnloadRenderTexture(n.mipTexture)
		}
		sc := int32(canvas.TexScale)
		logW := int32(NodeCols*n.CellW + 2*NodePad)
		logH := int32(n.maxRows*n.CellH + 2*NodePad)
		n.texture = rl.LoadRenderTexture(logW*sc, logH*sc)
		rl.SetTextureFilter(n.texture.Texture, rl.FilterBilinear)
		n.mip2Texture = rl.LoadRenderTexture(logW*2, logH*2)
		rl.SetTextureFilter(n.mip2Texture.Texture, rl.FilterBilinear)
		n.mipTexture = rl.LoadRenderTexture(logW, logH)
		rl.SetTextureFilter(n.mipTexture.Texture, rl.FilterBilinear)
		n.texValid = true
		n.dirty = true
	}
}

func (n *AgentNode) Contains(worldPoint rl.Vector2) bool {
	size := n.Size()
	return worldPoint.X >= n.Pos.X && worldPoint.X <= n.Pos.X+size.X &&
		worldPoint.Y >= n.Pos.Y && worldPoint.Y <= n.Pos.Y+size.Y
}

func (n *AgentNode) SetFocused(f bool) {
	if n.IsFocused != f {
		n.dirty = true
	}
	n.NodeBase.SetFocused(f)
}

func (n *AgentNode) Sandboxed() bool { return false }

func (n *AgentNode) Free() {
	if n.texValid {
		rl.UnloadRenderTexture(n.texture)
		rl.UnloadRenderTexture(n.mip2Texture)
		rl.UnloadRenderTexture(n.mipTexture)
		n.texValid = false
	}
}

// Events returns the channel for sending events to this node.
func (n *AgentNode) Events() chan proxy.AgentEvent {
	return n.events
}

// Done returns true if the stream has finished.
func (n *AgentNode) Done() bool {
	return n.done
}
