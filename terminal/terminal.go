package terminal

import (
	"claudehouse/ghostty"
	"math"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const Pad = 4

const (
	animNone  = 0
	animSpawn = 1
	animClose = 2
)

const (
	animSpawnDuration = 0.5
	animCloseDuration = 0.25
)

type TerminalNode struct {
	terminal     *ghostty.Terminal
	renderState  *ghostty.RenderState
	rowIter      *ghostty.RowIterator
	rowCells     *ghostty.RowCells
	keyEncoder   *ghostty.KeyEncoder
	keyEvent     *ghostty.KeyEvent
	mouseEncoder *ghostty.MouseEncoder
	mouseEvent   *ghostty.MouseEvent

	pty *PTY

	Pos        rl.Vector2
	cols, rows uint16
	cellW      int
	cellH      int
	fontSize   int
	font       rl.Font

	focused bool
	title   string

	animTime float64
	animType int
	closing  bool
	animDone bool

	scrollAccum float32
}

func NewTerminalNode(pos rl.Vector2, cols, rows uint16, font rl.Font, fontSize, cellW, cellH int, shell string) (*TerminalNode, error) {
	term, err := ghostty.NewTerminal(cols, rows, 1000)
	if err != nil {
		return nil, err
	}

	rs, err := ghostty.NewRenderState()
	if err != nil {
		term.Free()
		return nil, err
	}

	ri, err := ghostty.NewRowIterator()
	if err != nil {
		rs.Free()
		term.Free()
		return nil, err
	}

	rc, err := ghostty.NewRowCells()
	if err != nil {
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	ke, err := ghostty.NewKeyEncoder()
	if err != nil {
		rc.Free()
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	keyEvt, err := ghostty.NewKeyEvent()
	if err != nil {
		ke.Free()
		rc.Free()
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	me, err := ghostty.NewMouseEncoder()
	if err != nil {
		keyEvt.Free()
		ke.Free()
		rc.Free()
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	mouseEvt, err := ghostty.NewMouseEvent()
	if err != nil {
		me.Free()
		keyEvt.Free()
		ke.Free()
		rc.Free()
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	p, err := SpawnPTY(shell, cols, rows, cellW, cellH)
	if err != nil {
		mouseEvt.Free()
		me.Free()
		keyEvt.Free()
		ke.Free()
		rc.Free()
		ri.Free()
		rs.Free()
		term.Free()
		return nil, err
	}

	term.SetEffects(p.Fd(), cellW, cellH, cols, rows)

	return &TerminalNode{
		terminal:     term,
		renderState:  rs,
		rowIter:      ri,
		rowCells:     rc,
		keyEncoder:   ke,
		keyEvent:     keyEvt,
		mouseEncoder: me,
		mouseEvent:   mouseEvt,
		pty:          p,
		Pos:          pos,
		cols:         cols,
		rows:         rows,
		cellW:        cellW,
		cellH:        cellH,
		fontSize:     fontSize,
		font:         font,
	}, nil
}

func (tn *TerminalNode) Update() {
	// Tick animation
	if tn.animType != animNone {
		tn.animTime += float64(rl.GetFrameTime())
		switch tn.animType {
		case animSpawn:
			if tn.animTime >= animSpawnDuration {
				tn.animType = animNone
			}
		case animClose:
			if tn.animTime >= animCloseDuration {
				tn.animDone = true
			}
		}
	}

	// Smooth scroll: consume accumulated scroll lines
	if tn.scrollAccum != 0 {
		consume := tn.scrollAccum * 0.25
		if consume > -0.5 && consume < 0.5 {
			lines := int(math.Round(float64(tn.scrollAccum)))
			if lines != 0 {
				tn.terminal.ScrollViewport(lines)
			}
			tn.scrollAccum = 0
		} else {
			lines := int(consume)
			if lines != 0 {
				tn.terminal.ScrollViewport(lines)
				tn.scrollAccum -= float32(lines)
			}
		}
	}

	// Check if shell process exited
	if !tn.closing {
		select {
		case <-tn.pty.Done():
			tn.StartCloseAnim()
		default:
		}
	}

	// Drain PTY output into terminal
	for {
		select {
		case data := <-tn.pty.Output():
			tn.terminal.VTWrite(data)
		default:
			goto done
		}
	}
done:

	// Check title changes
	if title, changed := tn.terminal.GetTitle(); changed {
		tn.title = title
	}
}

func (tn *TerminalNode) Draw(camera rl.Camera2D) {
	tn.renderState.Update(tn.terminal)

	// Compute animation offset and alpha
	var offsetX, offsetY float32
	alpha := uint8(255)

	switch tn.animType {
	case animSpawn:
		t := tn.animTime
		if t < 0.2 {
			// Slide phase: Y +60 → 0, opacity 0 → 1
			p := t / 0.2
			ease := 1.0 - (1.0-p)*(1.0-p) // ease-out quadratic
			offsetY = float32((1.0 - ease) * 60.0)
			alpha = uint8(ease * 255.0)
		} else {
			// Jiggle phase: small X oscillation, damped sine
			jt := t - 0.2
			jDur := animSpawnDuration - 0.2
			p := jt / jDur
			damping := 1.0 - p
			offsetX = float32(damping * 8.0 * math.Sin(p*math.Pi*3))
		}
	case animClose:
		p := tn.animTime / animCloseDuration
		if p > 1.0 {
			p = 1.0
		}
		ease := p * p // ease-in quadratic
		offsetY = float32(ease * 40.0)
		alpha = uint8((1.0 - ease) * 255.0)
	}

	drawX := tn.Pos.X + offsetX
	drawY := tn.Pos.Y + offsetY

	// Calculate screen position including padding
	padX := int(drawX) + Pad
	padY := int(drawY) + Pad

	// Draw terminal background
	colors := tn.renderState.GetColors()
	bg := colors.Background
	width := int(tn.cols)*tn.cellW + 2*Pad
	height := int(tn.rows)*tn.cellH + 2*Pad
	rl.DrawRectangle(int32(drawX), int32(drawY), int32(width), int32(height),
		rl.Color{R: bg.R, G: bg.G, B: bg.B, A: alpha})

	// Draw border if focused
	if tn.focused {
		rl.DrawRectangleLines(int32(drawX)-1, int32(drawY)-1, int32(width)+2, int32(height)+2,
			rl.Color{R: 100, G: 150, B: 255, A: alpha})
	}

	DrawTerminal(tn.renderState, tn.rowIter, tn.rowCells, tn.font,
		tn.cellW, tn.cellH, tn.fontSize, padX, padY, alpha)
}

func (tn *TerminalNode) Closing() bool {
	return tn.closing
}

func (tn *TerminalNode) StartCloseAnim() {
	tn.closing = true
	tn.animType = animClose
	tn.animTime = 0
	tn.animDone = false
}

func (tn *TerminalNode) StartSpawnAnim() {
	tn.animType = animSpawn
	tn.animTime = 0
}

func (tn *TerminalNode) AnimDone() bool {
	return tn.animDone
}

var keysToCheck []int32

func init() {
	specialKeys := []int32{
		rl.KeySpace, rl.KeyEnter, rl.KeyTab, rl.KeyBackspace, rl.KeyDelete,
		rl.KeyEscape, rl.KeyUp, rl.KeyDown, rl.KeyLeft, rl.KeyRight,
		rl.KeyHome, rl.KeyEnd, rl.KeyPageUp, rl.KeyPageDown, rl.KeyInsert,
		rl.KeyMinus, rl.KeyEqual, rl.KeyLeftBracket, rl.KeyRightBracket,
		rl.KeyBackSlash, rl.KeySemicolon, rl.KeyApostrophe, rl.KeyComma,
		rl.KeyPeriod, rl.KeySlash, rl.KeyGrave,
		rl.KeyF1, rl.KeyF2, rl.KeyF3, rl.KeyF4, rl.KeyF5, rl.KeyF6,
		rl.KeyF7, rl.KeyF8, rl.KeyF9, rl.KeyF10, rl.KeyF11, rl.KeyF12,
	}
	keysToCheck = make([]int32, 0, 26+10+len(specialKeys))
	for k := int32(rl.KeyA); k <= int32(rl.KeyZ); k++ {
		keysToCheck = append(keysToCheck, k)
	}
	for k := int32(rl.KeyZero); k <= int32(rl.KeyNine); k++ {
		keysToCheck = append(keysToCheck, k)
	}
	keysToCheck = append(keysToCheck, specialKeys...)
}

func (tn *TerminalNode) processKey(rlKey int32, action ghostty.KeyAction, charUtf8 *[]byte) {
	gkey := raylibKeyToGhostty(rlKey)
	if gkey == ghostty.KeyUnidentified {
		return
	}

	mods := getGhosttyMods()

	tn.keyEvent.SetKey(gkey)
	tn.keyEvent.SetAction(action)
	tn.keyEvent.SetMods(mods)

	ucp := unshiftedCodepoint(rlKey)
	tn.keyEvent.SetUnshiftedCodepoint(ucp)

	var consumed ghostty.Mods
	if ucp != 0 && (mods&ghostty.ModsShift) != 0 {
		consumed |= ghostty.ModsShift
	}
	tn.keyEvent.SetConsumedMods(consumed)

	released := action == ghostty.KeyActionRelease

	if len(*charUtf8) > 0 && !released {
		tn.keyEvent.SetUtf8(*charUtf8)
		*charUtf8 = nil
	} else if cp := controlCharUtf8(rlKey); cp != 0 && !released {
		tn.keyEvent.SetUtf8([]byte{cp})
	} else {
		tn.keyEvent.SetUtf8(nil)
	}

	var buf [128]byte
	written := tn.keyEncoder.Encode(tn.keyEvent, buf[:])
	if written > 0 {
		tn.pty.Write(buf[:written])
		*charUtf8 = nil
	} else if !released {
		if cp := controlCharUtf8(rlKey); cp != 0 {
			tn.pty.Write([]byte{cp})
		}
	}
}

func (tn *TerminalNode) HandleKeyInput() {
	if tn.closing {
		return
	}
	tn.keyEncoder.SetOptFromTerminal(tn.terminal)

	// Drain printable character queue
	var charUtf8 []byte
	for {
		ch := rl.GetCharPressed()
		if ch == 0 {
			break
		}
		var u8 [4]byte
		n := utf8Encode(uint32(ch), u8[:])
		charUtf8 = append(charUtf8, u8[:n]...)
	}

	// Pass 1: Drain key press queue (event-based, never misses a press)
	for {
		rlKey := rl.GetKeyPressed()
		if rlKey == 0 {
			break
		}
		tn.processKey(rlKey, ghostty.KeyActionPress, &charUtf8)
	}

	// Pass 2: Check key repeats (poll-based, fine for repeats)
	for _, rlKey := range keysToCheck {
		if rl.IsKeyPressedRepeat(rlKey) {
			tn.processKey(rlKey, ghostty.KeyActionRepeat, &charUtf8)
		}
	}

	// Pass 3: Check key releases (poll-based, fine for releases)
	for _, rlKey := range keysToCheck {
		if rl.IsKeyReleased(rlKey) {
			tn.processKey(rlKey, ghostty.KeyActionRelease, &charUtf8)
		}
	}

	// Write any remaining unconsumed chars directly
	if len(charUtf8) > 0 {
		tn.pty.Write(charUtf8)
	}
}

func (tn *TerminalNode) HandleMouseInput(camera rl.Camera2D, mouseWorld rl.Vector2) {
	if tn.closing {
		return
	}
	tn.mouseEncoder.SetOptFromTerminal(tn.terminal)

	// Set encoder size relative to the terminal node's position
	width := int(tn.cols)*tn.cellW + 2*Pad
	height := int(tn.rows)*tn.cellH + 2*Pad
	tn.mouseEncoder.SetSize(width, height, tn.cellW, tn.cellH, Pad)

	anyPressed := rl.IsMouseButtonDown(rl.MouseButtonLeft) ||
		rl.IsMouseButtonDown(rl.MouseButtonRight) ||
		rl.IsMouseButtonDown(rl.MouseButtonMiddle)
	tn.mouseEncoder.SetAnyButtonPressed(anyPressed)
	tn.mouseEncoder.SetTrackLastCell(true)

	mods := getGhosttyMods()
	// Convert mouse position to terminal-local coordinates
	localX := mouseWorld.X - tn.Pos.X
	localY := mouseWorld.Y - tn.Pos.Y

	tn.mouseEvent.SetMods(mods)
	tn.mouseEvent.SetPosition(localX, localY)

	// Check buttons
	buttons := []rl.MouseButton{
		rl.MouseButtonLeft, rl.MouseButtonRight, rl.MouseButtonMiddle,
		rl.MouseButtonSide, rl.MouseButtonExtra, rl.MouseButtonForward,
		rl.MouseButtonBack,
	}
	for _, rlBtn := range buttons {
		gbtn := raylibMouseToGhostty(rlBtn)
		if gbtn == ghostty.MouseButtonUnknown {
			continue
		}
		if rl.IsMouseButtonPressed(rlBtn) {
			tn.mouseEvent.SetAction(ghostty.MouseActionPress)
			tn.mouseEvent.SetButton(gbtn)
			tn.mouseEncodeAndWrite()
		} else if rl.IsMouseButtonReleased(rlBtn) {
			tn.mouseEvent.SetAction(ghostty.MouseActionRelease)
			tn.mouseEvent.SetButton(gbtn)
			tn.mouseEncodeAndWrite()
		}
	}

	// Motion
	delta := rl.GetMouseDelta()
	if delta.X != 0 || delta.Y != 0 {
		tn.mouseEvent.SetAction(ghostty.MouseActionMotion)
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			tn.mouseEvent.SetButton(ghostty.MouseButtonLeft)
		} else if rl.IsMouseButtonDown(rl.MouseButtonRight) {
			tn.mouseEvent.SetButton(ghostty.MouseButtonRight)
		} else if rl.IsMouseButtonDown(rl.MouseButtonMiddle) {
			tn.mouseEvent.SetButton(ghostty.MouseButtonMiddle)
		} else {
			tn.mouseEvent.ClearButton()
		}
		tn.mouseEncodeAndWrite()
	}

	// Scroll wheel
	wheel := rl.GetMouseWheelMove()
	if wheel != 0 {
		if tn.terminal.GetMouseTracking() {
			var scrollBtn ghostty.MouseButton
			if wheel > 0 {
				scrollBtn = ghostty.MouseButtonFour
			} else {
				scrollBtn = ghostty.MouseButtonFive
			}
			tn.mouseEvent.SetButton(scrollBtn)
			tn.mouseEvent.SetAction(ghostty.MouseActionPress)
			tn.mouseEncodeAndWrite()
			tn.mouseEvent.SetAction(ghostty.MouseActionRelease)
			tn.mouseEncodeAndWrite()
		} else {
			tn.scrollAccum += wheel * -5.0
		}
	}
}

func (tn *TerminalNode) mouseEncodeAndWrite() {
	var buf [128]byte
	written := tn.mouseEncoder.Encode(tn.mouseEvent, buf[:])
	if written > 0 {
		tn.pty.Write(buf[:written])
	}
}

func (tn *TerminalNode) Position() rl.Vector2 {
	return tn.Pos
}

func (tn *TerminalNode) SetPosition(pos rl.Vector2) {
	tn.Pos = pos
}

func (tn *TerminalNode) Size() rl.Vector2 {
	width := float32(int(tn.cols)*tn.cellW + 2*Pad)
	height := float32(int(tn.rows)*tn.cellH + 2*Pad)
	return rl.Vector2{X: width, Y: height}
}

func (tn *TerminalNode) SetSize(cols, rows uint16) {
	if cols < 2 {
		cols = 2
	}
	if rows < 2 {
		rows = 2
	}
	tn.cols = cols
	tn.rows = rows
	tn.terminal.Resize(cols, rows, tn.cellW, tn.cellH)
	tn.terminal.UpdateEffectsSize(cols, rows)
	tn.pty.Resize(cols, rows, tn.cellW, tn.cellH)
}

func (tn *TerminalNode) CellSize() (int, int) {
	return tn.cellW, tn.cellH
}

func (tn *TerminalNode) Contains(worldPoint rl.Vector2) bool {
	size := tn.Size()
	return worldPoint.X >= tn.Pos.X && worldPoint.X <= tn.Pos.X+size.X &&
		worldPoint.Y >= tn.Pos.Y && worldPoint.Y <= tn.Pos.Y+size.Y
}

func (tn *TerminalNode) Focused() bool {
	return tn.focused
}

func (tn *TerminalNode) SetFocused(f bool) {
	tn.focused = f
}

func (tn *TerminalNode) Free() {
	tn.pty.Close()
	tn.mouseEvent.Free()
	tn.mouseEncoder.Free()
	tn.keyEvent.Free()
	tn.keyEncoder.Free()
	tn.rowCells.Free()
	tn.rowIter.Free()
	tn.renderState.Free()
	tn.terminal.Free()
}
