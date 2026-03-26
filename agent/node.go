package agent

import (
	"fmt"
	"strings"
	"sync"

	"claudehouse/proxy"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	NodePad    = 8
	NodeCols   = 60
	animNone   = 0
	animSpawn  = 1
	animClose  = 2

	animSpawnDuration = 0.5
	animCloseDuration = 0.25
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
	mu sync.Mutex

	StreamID       string
	TerminalID     string
	IsSubagent     bool
	ParentStreamID string
	Number         int    // sequential agent number within terminal
	ParentLabel    string // e.g. "#1" if parent is agent #1
	Model          string // model name from stream_start

	Pos      rl.Vector2
	font     rl.Font
	fontSize int
	cellW    int
	cellH    int

	blocks []*ContentBlock

	// Scroll
	scrollOffset int
	maxRows      int

	// Animation
	animType int
	animTime float64
	closing  bool
	animDone bool
	focused  bool

	// Event channel for receiving updates
	events   chan proxy.AgentEvent
	done     bool    // stream finished
	doneTime float64 // time when stream_end was received
}

// NewAgentNode creates an agent output node.
func NewAgentNode(pos rl.Vector2, font rl.Font, fontSize, cellW, cellH int, streamID, terminalID string, isSubagent bool) *AgentNode {
	return &AgentNode{
		StreamID:   streamID,
		TerminalID: terminalID,
		IsSubagent: isSubagent,
		Pos:        pos,
		font:       font,
		fontSize:   fontSize,
		cellW:      cellW,
		cellH:      cellH,
		maxRows:    30,
		events:     make(chan proxy.AgentEvent, 64),
	}
}

func (n *AgentNode) Update() {
	// Tick animation
	if n.animType != animNone {
		n.animTime += float64(rl.GetFrameTime())
		switch n.animType {
		case animSpawn:
			if n.animTime >= animSpawnDuration {
				n.animType = animNone
			}
		case animClose:
			if n.animTime >= animCloseDuration {
				n.animDone = true
			}
		}
	}

	// Auto-despawn after 3 seconds of being done
	if n.done && !n.closing && n.doneTime > 0 {
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
}

func (n *AgentNode) lastBlock(typ string) *ContentBlock {
	for i := len(n.blocks) - 1; i >= 0; i-- {
		if n.blocks[i].Type == typ && !n.blocks[i].Complete {
			return n.blocks[i]
		}
	}
	return nil
}

func (n *AgentNode) Draw(camera rl.Camera2D) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Animation
	var offsetY float32
	alpha := uint8(255)

	switch n.animType {
	case animSpawn:
		t := n.animTime
		if t < 0.2 {
			p := t / 0.2
			ease := 1.0 - (1.0-p)*(1.0-p)
			offsetY = float32((1.0 - ease) * 60.0)
			alpha = uint8(ease * 255.0)
		}
	case animClose:
		p := n.animTime / animCloseDuration
		if p > 1.0 {
			p = 1.0
		}
		ease := p * p
		offsetY = float32(ease * 40.0)
		alpha = uint8((1.0 - ease) * 255.0)
	}

	drawX := n.Pos.X
	drawY := n.Pos.Y + offsetY

	width := float32(NodeCols*n.cellW + 2*NodePad)

	// Render all blocks and calculate total height.
	var lines []renderedLine
	for _, b := range n.blocks {
		lines = append(lines, n.renderBlock(b)...)
	}

	// Header line
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

	totalRows := 1 + len(lines) // header + content
	if totalRows > n.maxRows {
		totalRows = n.maxRows
	}
	height := float32(totalRows*n.cellH + 2*NodePad)

	// Background
	rl.DrawRectangle(int32(drawX), int32(drawY), int32(width), int32(height),
		rl.Color{R: 25, G: 25, B: 35, A: alpha})

	// Border
	borderColor := rl.Color{R: 160, G: 100, B: 255, A: alpha} // purple
	if n.IsSubagent {
		borderColor = rl.Color{R: 80, G: 200, B: 220, A: alpha} // cyan
	}
	if n.done {
		borderColor.A = alpha / 2
	}
	rl.DrawRectangleLines(int32(drawX), int32(drawY), int32(width), int32(height), borderColor)

	// Draw header
	px := int32(drawX) + int32(NodePad)
	py := int32(drawY) + int32(NodePad)
	rl.DrawTextEx(n.font, headerText, rl.Vector2{X: float32(px), Y: float32(py)},
		float32(n.fontSize), 0, rl.Color{R: borderColor.R, G: borderColor.G, B: borderColor.B, A: alpha})
	py += int32(n.cellH)

	// Draw content lines (with scroll offset)
	startLine := n.scrollOffset
	visibleRows := totalRows - 1 // minus header
	if startLine > len(lines)-visibleRows {
		startLine = len(lines) - visibleRows
	}
	if startLine < 0 {
		startLine = 0
	}

	for i := startLine; i < len(lines) && (i-startLine) < visibleRows; i++ {
		line := lines[i]
		rl.DrawTextEx(n.font, line.text, rl.Vector2{X: float32(px), Y: float32(py)},
			float32(n.fontSize), 0, rl.Color{R: line.color.R, G: line.color.G, B: line.color.B, A: alpha})
		py += int32(n.cellH)
	}
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
	// Scroll with page up/down when focused
	if n.focused {
		if rl.IsKeyPressed(rl.KeyPageUp) {
			n.scrollOffset -= 10
			if n.scrollOffset < 0 {
				n.scrollOffset = 0
			}
		}
		if rl.IsKeyPressed(rl.KeyPageDown) {
			n.scrollOffset += 10
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
	}
}

func (n *AgentNode) Position() rl.Vector2    { return n.Pos }
func (n *AgentNode) SetPosition(p rl.Vector2) { n.Pos = p }

func (n *AgentNode) Size() rl.Vector2 {
	width := float32(NodeCols*n.cellW + 2*NodePad)
	totalRows := n.maxRows
	height := float32(totalRows*n.cellH + 2*NodePad)
	return rl.Vector2{X: width, Y: height}
}

func (n *AgentNode) SetSize(cols, rows uint16) {
	n.maxRows = int(rows)
}

func (n *AgentNode) CellSize() (int, int) {
	return n.cellW, n.cellH
}

func (n *AgentNode) Contains(worldPoint rl.Vector2) bool {
	size := n.Size()
	return worldPoint.X >= n.Pos.X && worldPoint.X <= n.Pos.X+size.X &&
		worldPoint.Y >= n.Pos.Y && worldPoint.Y <= n.Pos.Y+size.Y
}

func (n *AgentNode) Focused() bool      { return n.focused }
func (n *AgentNode) SetFocused(f bool)   { n.focused = f }
func (n *AgentNode) Closing() bool       { return n.closing }
func (n *AgentNode) AnimDone() bool      { return n.animDone }
func (n *AgentNode) Sandboxed() bool     { return false }
func (n *AgentNode) Free()               {}

func (n *AgentNode) StartCloseAnim() {
	n.closing = true
	n.animType = animClose
	n.animTime = 0
	n.animDone = false
}

func (n *AgentNode) StartSpawnAnim() {
	n.animType = animSpawn
	n.animTime = 0
}

// Events returns the channel for sending events to this node.
func (n *AgentNode) Events() chan proxy.AgentEvent {
	return n.events
}

// Done returns true if the stream has finished.
func (n *AgentNode) Done() bool {
	return n.done
}
