package terminal

import (
	"claudehouse/ghostty"
	"math"
	"os/exec"
	"strings"
	"time"

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
	sandboxed   bool

	// Text selection state
	selecting    bool
	selHasRange  bool
	selStartCol  int
	selStartRow  int
	selEndCol    int
	selEndRow    int
	selCopyTimer float64 // debounce timer for clipboard copy
}

func NewTerminalNode(pos rl.Vector2, cols, rows uint16, font rl.Font, fontSize, cellW, cellH int, shell string, sandboxed bool, proxyEnv []string) (*TerminalNode, error) {
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

	var p *PTY
	if sandboxed {
		p, err = SpawnSandboxedPTY(shell, cols, rows, cellW, cellH, proxyEnv)
	} else {
		p, err = SpawnPTY(shell, cols, rows, cellW, cellH, proxyEnv)
	}
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
		sandboxed:    sandboxed,
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

	// Draw border if focused (orange for sandboxed, blue for normal)
	if tn.focused {
		borderColor := rl.Color{R: 100, G: 150, B: 255, A: alpha}
		if tn.sandboxed {
			borderColor = rl.Color{R: 255, G: 160, B: 40, A: alpha}
		}
		rl.DrawRectangleLines(int32(drawX)-1, int32(drawY)-1, int32(width)+2, int32(height)+2, borderColor)
	}

	sc, sr, ec, er, selActive := tn.Selection()
	sel := SelectionRange{
		StartCol: sc, StartRow: sr,
		EndCol: ec, EndRow: er,
		Active: selActive,
	}
	DrawTerminal(tn.renderState, tn.rowIter, tn.rowCells, tn.font,
		tn.cellW, tn.cellH, tn.fontSize, padX, padY, alpha, sel)
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
	if ucp > 0x20 && (mods&ghostty.ModsShift) != 0 {
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

// mouseToCellPos converts world-space mouse coordinates to terminal cell col/row.
func (tn *TerminalNode) mouseToCellPos(mouseWorld rl.Vector2) (col, row int) {
	localX := mouseWorld.X - tn.Pos.X - float32(Pad)
	localY := mouseWorld.Y - tn.Pos.Y - float32(Pad)
	col = int(localX) / tn.cellW
	row = int(localY) / tn.cellH
	if col < 0 {
		col = 0
	}
	if col >= int(tn.cols) {
		col = int(tn.cols) - 1
	}
	if row < 0 {
		row = 0
	}
	if row >= int(tn.rows) {
		row = int(tn.rows) - 1
	}
	return
}

// Selection returns the current selection range, normalized so start <= end.
func (tn *TerminalNode) Selection() (startCol, startRow, endCol, endRow int, active bool) {
	if !tn.selHasRange {
		return 0, 0, 0, 0, false
	}
	sr, sc := tn.selStartRow, tn.selStartCol
	er, ec := tn.selEndRow, tn.selEndCol
	// Normalize: start before end
	if sr > er || (sr == er && sc > ec) {
		sr, sc, er, ec = er, ec, sr, sc
	}
	return sc, sr, ec, er, true
}

// ClearSelection clears the current text selection.
func (tn *TerminalNode) ClearSelection() {
	tn.selecting = false
	tn.selHasRange = false
}

const copyDebounce = 150 * time.Millisecond

func (tn *TerminalNode) HandleMouseInput(camera rl.Camera2D, mouseWorld rl.Vector2) {
	if tn.closing {
		return
	}

	mouseTracking := tn.terminal.GetMouseTracking()

	// --- Text selection (only when terminal is NOT tracking mouse) ---
	if !mouseTracking {
		if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
			col, row := tn.mouseToCellPos(mouseWorld)
			tn.selecting = true
			tn.selStartCol = col
			tn.selStartRow = row
			tn.selEndCol = col
			tn.selEndRow = row
			tn.selHasRange = false
			tn.selCopyTimer = 0
		}
		if tn.selecting && rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			col, row := tn.mouseToCellPos(mouseWorld)
			if col != tn.selEndCol || row != tn.selEndRow {
				tn.selEndCol = col
				tn.selEndRow = row
				tn.selHasRange = true
			}
		}
		if tn.selecting && rl.IsMouseButtonReleased(rl.MouseButtonLeft) {
			tn.selecting = false
			if tn.selHasRange {
				tn.selCopyTimer = float64(rl.GetTime())
			}
		}
		// Debounced copy to clipboard
		if tn.selCopyTimer > 0 && float64(rl.GetTime())-tn.selCopyTimer >= copyDebounce.Seconds() {
			tn.selCopyTimer = 0
			go tn.copySelectionToClipboard()
		}
	} else {
		// Clear selection when entering mouse tracking mode
		tn.ClearSelection()
	}

	// --- Forward mouse events to terminal ---
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

	// Check buttons — skip left button when not in mouse tracking mode (used for selection)
	buttons := []rl.MouseButton{
		rl.MouseButtonLeft, rl.MouseButtonRight, rl.MouseButtonMiddle,
		rl.MouseButtonSide, rl.MouseButtonExtra, rl.MouseButtonForward,
		rl.MouseButtonBack,
	}
	for _, rlBtn := range buttons {
		if rlBtn == rl.MouseButtonLeft && !mouseTracking {
			continue // left button is used for text selection
		}
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

	// Motion — skip left-button motion when not in mouse tracking mode
	delta := rl.GetMouseDelta()
	if delta.X != 0 || delta.Y != 0 {
		tn.mouseEvent.SetAction(ghostty.MouseActionMotion)
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) && mouseTracking {
			tn.mouseEvent.SetButton(ghostty.MouseButtonLeft)
		} else if rl.IsMouseButtonDown(rl.MouseButtonRight) {
			tn.mouseEvent.SetButton(ghostty.MouseButtonRight)
		} else if rl.IsMouseButtonDown(rl.MouseButtonMiddle) {
			tn.mouseEvent.SetButton(ghostty.MouseButtonMiddle)
		} else {
			tn.mouseEvent.ClearButton()
		}
		if mouseTracking || !rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			tn.mouseEncodeAndWrite()
		}
	}

	// Scroll wheel
	wheel := rl.GetMouseWheelMove()
	if wheel != 0 {
		if mouseTracking {
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

// copySelectionToClipboard extracts the selected text and copies it via wl-copy.
func (tn *TerminalNode) copySelectionToClipboard() {
	sc, sr, ec, er, ok := tn.Selection()
	if !ok {
		return
	}

	// We need to read cells from the render state, which must be done
	// with an up-to-date render state. Create temporary iterators.
	rs, err := ghostty.NewRenderState()
	if err != nil {
		return
	}
	defer rs.Free()

	ri, err := ghostty.NewRowIterator()
	if err != nil {
		return
	}
	defer ri.Free()

	rc, err := ghostty.NewRowCells()
	if err != nil {
		return
	}
	defer rc.Free()

	rs.Update(tn.terminal)
	if !ri.Init(rs) {
		return
	}

	var lines []string
	row := 0
	var cpBuf [16]uint32
	var textBuf [64]byte

	for ri.Next() {
		if row < sr {
			row++
			continue
		}
		if row > er {
			break
		}

		if !ri.GetCells(rc) {
			row++
			continue
		}

		startC := 0
		endC := int(tn.cols) - 1
		if row == sr {
			startC = sc
		}
		if row == er {
			endC = ec
		}

		var line strings.Builder
		for col := startC; col <= endC; col++ {
			if !rc.Select(uint16(col)) {
				line.WriteByte(' ')
				continue
			}
			graphemeLen := rc.GraphemeLen()
			if graphemeLen == 0 {
				line.WriteByte(' ')
				continue
			}
			length := int(graphemeLen)
			if length > 16 {
				length = 16
			}
			rc.Graphemes(cpBuf[:length])
			pos := 0
			for i := 0; i < length && pos < 60; i++ {
				n := utf8Encode(cpBuf[i], textBuf[pos:])
				pos += n
			}
			line.Write(textBuf[:pos])
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
		row++
	}

	text := strings.Join(lines, "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}

	cmd := exec.Command("wl-copy", text)
	cmd.Run()
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

func (tn *TerminalNode) Sandboxed() bool {
	return tn.sandboxed
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
