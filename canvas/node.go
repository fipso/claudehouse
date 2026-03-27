package canvas

import rl "github.com/gen2brain/raylib-go/raylib"

type Node interface {
	Update()
	PreDraw()
	Draw(camera rl.Camera2D)
	HandleKeyInput()
	HandleMouseInput(camera rl.Camera2D, mouseWorld rl.Vector2)
	Position() rl.Vector2
	SetPosition(rl.Vector2)
	Size() rl.Vector2
	SetSize(cols, rows uint16)
	CellSize() (int, int)
	Contains(worldPoint rl.Vector2) bool
	Focused() bool
	SetFocused(bool)
	Free()
	Closing() bool
	StartCloseAnim()
	StartSpawnAnim()
	AnimDone() bool
}

const (
	AnimNone  = 0
	AnimSpawn = 1
	AnimClose = 2

	AnimSpawnDuration = 0.5
	AnimCloseDuration = 0.25
)

// NodeBase contains shared animation, position, and lifecycle state
// embedded by both TerminalNode and AgentNode.
// TexScale is the render texture resolution multiplier.
// Matches the font's 4x load size for crisp rendering at any zoom.
const TexScale = 4

type NodeBase struct {
	Pos      rl.Vector2
	Font     rl.Font
	FontSize int
	CellW    int
	CellH    int

	AnimType   int
	AnimTime   float64
	IsClosing  bool
	IsAnimDone bool
	IsFocused  bool
}

// TickAnim advances the animation timer and transitions state.
func (nb *NodeBase) TickAnim() {
	if nb.AnimType == AnimNone {
		return
	}
	nb.AnimTime += float64(rl.GetFrameTime())
	switch nb.AnimType {
	case AnimSpawn:
		if nb.AnimTime >= AnimSpawnDuration {
			nb.AnimType = AnimNone
		}
	case AnimClose:
		if nb.AnimTime >= AnimCloseDuration {
			nb.IsAnimDone = true
		}
	}
}

// AnimOffset computes animation offset and alpha for the current frame.
// For spawn: handles the slide phase (first 0.2s). After 0.2s returns no offset
// (TerminalNode adds jiggle on top). For close: full ease-in.
func (nb *NodeBase) AnimOffset() (offsetX, offsetY float32, alpha uint8) {
	alpha = 255
	switch nb.AnimType {
	case AnimSpawn:
		t := nb.AnimTime
		if t < 0.2 {
			p := t / 0.2
			ease := 1.0 - (1.0-p)*(1.0-p)
			offsetY = float32((1.0 - ease) * 60.0)
			alpha = uint8(ease * 255.0)
		}
	case AnimClose:
		p := nb.AnimTime / AnimCloseDuration
		if p > 1.0 {
			p = 1.0
		}
		ease := p * p
		offsetY = float32(ease * 40.0)
		alpha = uint8((1.0 - ease) * 255.0)
	}
	return
}

func (nb *NodeBase) StartCloseAnim() {
	nb.IsClosing = true
	nb.AnimType = AnimClose
	nb.AnimTime = 0
	nb.IsAnimDone = false
}

func (nb *NodeBase) StartSpawnAnim() {
	nb.AnimType = AnimSpawn
	nb.AnimTime = 0
}

func (nb *NodeBase) Closing() bool       { return nb.IsClosing }
func (nb *NodeBase) AnimDone() bool      { return nb.IsAnimDone }
func (nb *NodeBase) Focused() bool       { return nb.IsFocused }
func (nb *NodeBase) SetFocused(f bool)   { nb.IsFocused = f }
func (nb *NodeBase) Position() rl.Vector2    { return nb.Pos }
func (nb *NodeBase) SetPosition(p rl.Vector2) { nb.Pos = p }
func (nb *NodeBase) CellSize() (int, int)    { return nb.CellW, nb.CellH }
