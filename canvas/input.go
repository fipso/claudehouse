package canvas

import (
	"math"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

var GridSize float32 = 50
var SnapEnabled bool = true
var ZoomToFitGap int = 100
var FillViewportGap int = 40

const terminalPad = 4 // must match terminal.Pad

func snapToGrid(v rl.Vector2) rl.Vector2 {
	if !SnapEnabled {
		return v
	}
	return rl.Vector2{
		X: float32(math.Round(float64(v.X/GridSize))) * GridSize,
		Y: float32(math.Round(float64(v.Y/GridSize))) * GridSize,
	}
}

var lastEscapeTime time.Time
var lastClickTime time.Time
var lastClickPos rl.Vector2

// SandboxMode controls whether new terminals are created inside a gVisor sandbox.
var SandboxMode bool

// CreateNodeFunc is set by main to create new terminal nodes.
var CreateNodeFunc func(pos rl.Vector2) Node

func (c *Canvas) HandleInput() {
	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())

	c.Camera.Offset = rl.Vector2{
		X: screenW / 2,
		Y: screenH / 2,
	}

	mousePos := rl.GetMousePosition()
	mouseWorld := rl.GetScreenToWorld2D(mousePos, c.Camera)

	if c.FocusedIdx >= 0 && c.FocusedIdx < len(c.Nodes) {
		c.handleFocusedInput(mousePos, mouseWorld)
	} else {
		c.handleCanvasInput(mousePos, mouseWorld)
	}
}

func (c *Canvas) handleFocusedInput(mousePos, mouseWorld rl.Vector2) {
	node := c.Nodes[c.FocusedIdx]

	// If focused node is closing, unfocus and bail
	if node.Closing() {
		c.FocusedIdx = -1
		return
	}

	// Active resize drag
	if c.resizing {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			dx := mouseWorld.X - c.resizeAnchor.X
			dy := mouseWorld.Y - c.resizeAnchor.Y
			cellW, cellH := node.CellSize()
			cw := float32(cellW)
			ch := float32(cellH)

			newCols := c.resizeStartW
			newRows := c.resizeStartH
			newPos := c.resizeStartPos

			if c.resizeEdges[1] { // right
				dc := int16(dx / cw)
				nc := int16(c.resizeStartW) + dc
				if nc < 2 {
					nc = 2
				}
				newCols = uint16(nc)
			}
			if c.resizeEdges[3] { // left
				dc := int16(dx / cw)
				nc := int16(c.resizeStartW) - dc
				if nc < 2 {
					nc = 2
				}
				newCols = uint16(nc)
				newPos.X = c.resizeStartPos.X + float32(int16(c.resizeStartW)-int16(newCols))*cw
			}
			if c.resizeEdges[2] { // bottom
				dr := int16(dy / ch)
				nr := int16(c.resizeStartH) + dr
				if nr < 2 {
					nr = 2
				}
				newRows = uint16(nr)
			}
			if c.resizeEdges[0] { // top
				dr := int16(dy / ch)
				nr := int16(c.resizeStartH) - dr
				if nr < 2 {
					nr = 2
				}
				newRows = uint16(nr)
				newPos.Y = c.resizeStartPos.Y + float32(int16(c.resizeStartH)-int16(newRows))*ch
			}

			node.SetSize(newCols, newRows)
			node.SetPosition(newPos)
		} else {
			// Snap position on release
			node.SetPosition(snapToGrid(node.Position()))
			c.resizing = false
		}
		return
	}

	// Double-Escape to unfocus
	if rl.IsKeyPressed(rl.KeyEscape) {
		now := time.Now()
		if now.Sub(lastEscapeTime) < 300*time.Millisecond {
			node.SetFocused(false)
			c.FocusedIdx = -1
			lastEscapeTime = time.Time{}
			return
		}
		lastEscapeTime = now
	}

	// Click on edge of focused node → start resize; click outside → unfocus
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		pos := node.Position()
		size := node.Size()
		const edgeThreshold = 6

		onLeft := mouseWorld.X >= pos.X-edgeThreshold && mouseWorld.X <= pos.X+edgeThreshold
		onRight := mouseWorld.X >= pos.X+size.X-edgeThreshold && mouseWorld.X <= pos.X+size.X+edgeThreshold
		onTop := mouseWorld.Y >= pos.Y-edgeThreshold && mouseWorld.Y <= pos.Y+edgeThreshold
		onBottom := mouseWorld.Y >= pos.Y+size.Y-edgeThreshold && mouseWorld.Y <= pos.Y+size.Y+edgeThreshold
		inYRange := mouseWorld.Y >= pos.Y-edgeThreshold && mouseWorld.Y <= pos.Y+size.Y+edgeThreshold
		inXRange := mouseWorld.X >= pos.X-edgeThreshold && mouseWorld.X <= pos.X+size.X+edgeThreshold

		onEdge := (onLeft && inYRange) || (onRight && inYRange) || (onTop && inXRange) || (onBottom && inXRange)

		if onEdge {
			cellW, cellH := node.CellSize()
			c.resizing = true
			c.resizeEdges = [4]bool{onTop && inXRange, onRight && inYRange, onBottom && inXRange, onLeft && inYRange}
			c.resizeStartPos = pos
			c.resizeStartW = uint16((int(size.X) - 2*terminalPad) / cellW)
			c.resizeStartH = uint16((int(size.Y) - 2*terminalPad) / cellH)
			c.resizeAnchor = mouseWorld
			return
		}

		if !node.Contains(mouseWorld) {
			node.SetFocused(false)
			c.FocusedIdx = -1
			// Check if clicking on another node
			for i := len(c.Nodes) - 1; i >= 0; i-- {
				if c.Nodes[i].Closing() {
					continue
				}
				if c.Nodes[i].Contains(mouseWorld) {
					c.FocusedIdx = i
					c.Nodes[i].SetFocused(true)
					return
				}
			}
			// No node hit — start camera drag and fall through
			c.dragging = true
			c.animatingPos = false
			return
		}
	}

	// Alt+HJKL → directional focus navigation
	// Alt+Q → close focused terminal
	// Alt+Enter → spawn terminal to the right
	if rl.IsKeyDown(rl.KeyLeftAlt) {
		switch {
		case rl.IsKeyPressed(rl.KeyH):
			c.focusDirection(-1, 0)
			return
		case rl.IsKeyPressed(rl.KeyJ):
			c.focusDirection(0, 1)
			return
		case rl.IsKeyPressed(rl.KeyK):
			c.focusDirection(0, -1)
			return
		case rl.IsKeyPressed(rl.KeyL):
			c.focusDirection(1, 0)
			return
		case rl.IsKeyPressed(rl.KeyQ):
			c.RemoveNode(c.FocusedIdx)
			return
		case rl.IsKeyPressed(rl.KeyS):
			SandboxMode = !SandboxMode
			return
		case rl.IsKeyPressed(rl.KeyE):
			c.zoomToFit(node)
			return
		case rl.IsKeyPressed(rl.KeyF):
			c.fillViewport(node)
			return
		case rl.IsKeyPressed(rl.KeyEqual): // + key
			c.zoomStep(1, mousePos)
			return
		case rl.IsKeyPressed(rl.KeyMinus):
			c.zoomStep(-1, mousePos)
			return
		case rl.IsKeyPressed(rl.KeyEnter):
			// Snap any in-progress node animation so position is final
			if c.nodeAnimIdx >= 0 && c.nodeAnimIdx < len(c.Nodes) {
				c.Nodes[c.nodeAnimIdx].SetPosition(c.nodeAnimTarget)
				c.nodeAnimIdx = -1
			}
			pos := node.Position()
			size := node.Size()
			cellW, cellH := node.CellSize()
			cols := uint16((int(size.X) - 2*terminalPad) / cellW)
			rows := uint16((int(size.Y) - 2*terminalPad) / cellH)
			newPos := rl.Vector2{X: snapToGrid(rl.Vector2{X: pos.X + size.X + GridSize}).X, Y: pos.Y}
			if !c.wouldOverlap(newPos, size) && CreateNodeFunc != nil {
				newNode := CreateNodeFunc(newPos)
				if newNode != nil {
					newNode.SetSize(cols, rows)
					node.SetFocused(false)
					c.AddNode(newNode)
					c.FocusedIdx = len(c.Nodes) - 1
					newNode.SetFocused(true)
				}
			}
			return
		}
	}

	// Forward input to focused node
	node.HandleKeyInput()
	node.HandleMouseInput(c.Camera, mouseWorld)
}

func (c *Canvas) handleCanvasInput(mousePos, mouseWorld rl.Vector2) {
	// Arrow keys → pan camera (cancels any smooth pan animation)
	panSpeed := float32(500) * rl.GetFrameTime() / c.Camera.Zoom
	if rl.IsKeyDown(rl.KeyUp) {
		c.Camera.Target.Y -= panSpeed
		c.animatingPos = false
	}
	if rl.IsKeyDown(rl.KeyDown) {
		c.Camera.Target.Y += panSpeed
		c.animatingPos = false
	}
	if rl.IsKeyDown(rl.KeyLeft) {
		c.Camera.Target.X -= panSpeed
		c.animatingPos = false
	}
	if rl.IsKeyDown(rl.KeyRight) {
		c.Camera.Target.X += panSpeed
	}

	// Scroll wheel → smooth zoom toward mouse
	wheel := rl.GetMouseWheelMove()
	if wheel != 0 {
		c.zoomMousePos = mousePos
		c.targetZoom *= (1 + wheel*0.15)
		if c.targetZoom < 0.1 {
			c.targetZoom = 0.1
		}
		if c.targetZoom > 5.0 {
			c.targetZoom = 5.0
		}
	}

	// Alt keybinds in unfocused mode
	if rl.IsKeyDown(rl.KeyLeftAlt) {
		switch {
		case rl.IsKeyPressed(rl.KeyH):
			c.focusDirection(-1, 0)
			return
		case rl.IsKeyPressed(rl.KeyJ):
			c.focusDirection(0, 1)
			return
		case rl.IsKeyPressed(rl.KeyK):
			c.focusDirection(0, -1)
			return
		case rl.IsKeyPressed(rl.KeyL):
			c.focusDirection(1, 0)
			return
		case rl.IsKeyPressed(rl.KeyEqual):
			c.zoomStep(1, mousePos)
		case rl.IsKeyPressed(rl.KeyMinus):
			c.zoomStep(-1, mousePos)
		case rl.IsKeyPressed(rl.KeyS):
			SandboxMode = !SandboxMode
			return
		}
	}

	// --- Active drags: group move, single-node move, camera pan, selection ---

	if c.movingGroup {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			delta := rl.Vector2{
				X: mouseWorld.X - c.groupMoveStart.X,
				Y: mouseWorld.Y - c.groupMoveStart.Y,
			}
			for j, idx := range c.Selected {
				if idx >= 0 && idx < len(c.Nodes) {
					c.Nodes[idx].SetPosition(rl.Vector2{
						X: c.groupMoveBase[j].X + delta.X,
						Y: c.groupMoveBase[j].Y + delta.Y,
					})
				}
			}
		} else {
			// Snap all group nodes to grid on release
			for _, idx := range c.Selected {
				if idx >= 0 && idx < len(c.Nodes) {
					c.Nodes[idx].SetPosition(snapToGrid(c.Nodes[idx].Position()))
				}
			}
			c.movingGroup = false
		}
		return
	}

	if c.movingNode >= 0 {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			c.Nodes[c.movingNode].SetPosition(rl.Vector2{
				X: mouseWorld.X - c.moveOffset.X,
				Y: mouseWorld.Y - c.moveOffset.Y,
			})
		} else {
			// Snap to grid on release
			c.Nodes[c.movingNode].SetPosition(snapToGrid(c.Nodes[c.movingNode].Position()))
			c.movingNode = -1
		}
		return
	}

	if c.dragging {
		if rl.IsMouseButtonDown(rl.MouseButtonLeft) {
			delta := rl.GetMouseDelta()
			c.Camera.Target.X -= delta.X / c.Camera.Zoom
			c.Camera.Target.Y -= delta.Y / c.Camera.Zoom
		} else {
			c.dragging = false
		}
		return
	}

	if c.selecting {
		c.selectEnd = mouseWorld
		c.updateSelectionFromRect()
		if !rl.IsMouseButtonDown(rl.MouseButtonRight) {
			c.selecting = false
		}
		return
	}

	// --- New press events ---

	// Left-click press
	if rl.IsMouseButtonPressed(rl.MouseButtonLeft) {
		// Alt+left-click → single node move
		if rl.IsKeyDown(rl.KeyLeftAlt) {
			for i := len(c.Nodes) - 1; i >= 0; i-- {
				if c.Nodes[i].Closing() {
					continue
				}
				if c.Nodes[i].Contains(mouseWorld) {
					c.movingNode = i
					nodePos := c.Nodes[i].Position()
					c.moveOffset = rl.Vector2{
						X: mouseWorld.X - nodePos.X,
						Y: mouseWorld.Y - nodePos.Y,
					}
					if c.FocusedIdx == i {
						c.Nodes[i].SetFocused(false)
						c.FocusedIdx = -1
					}
					return
				}
			}
		}

		// Check if clicking on a node
		hitIdx := -1
		for i := len(c.Nodes) - 1; i >= 0; i-- {
			if c.Nodes[i].Closing() {
				continue
			}
			if c.Nodes[i].Contains(mouseWorld) {
				hitIdx = i
				break
			}
		}

		if hitIdx >= 0 {
			// Clicked on a selected node → start group move
			if c.isSelected(hitIdx) {
				c.movingGroup = true
				c.groupMoveStart = mouseWorld
				c.groupMoveBase = make([]rl.Vector2, len(c.Selected))
				for j, idx := range c.Selected {
					c.groupMoveBase[j] = c.Nodes[idx].Position()
				}
				return
			}
			// Clicked on unselected node → clear selection, focus it
			c.Selected = nil
			c.FocusedIdx = hitIdx
			c.Nodes[hitIdx].SetFocused(true)
		} else {
			// Clicked on background → check for double-click to create terminal
			now := time.Now()
			dx := mouseWorld.X - lastClickPos.X
			dy := mouseWorld.Y - lastClickPos.Y
			dist := dx*dx + dy*dy
			if now.Sub(lastClickTime) < 300*time.Millisecond && dist < 100 && CreateNodeFunc != nil {
				node := CreateNodeFunc(snapToGrid(mouseWorld))
				if node != nil {
					c.AddNode(node)
				}
				lastClickTime = time.Time{}
				return
			}
			lastClickTime = now
			lastClickPos = mouseWorld

			// Start camera drag
			c.Selected = nil
			c.dragging = true
			c.animatingPos = false
		}
		return
	}

	// Right-click → rubber-band selection
	if rl.IsMouseButtonPressed(rl.MouseButtonRight) {
		c.selecting = true
		c.selectStart = mouseWorld
		c.selectEnd = mouseWorld
		c.Selected = nil
		return
	}
}

func (c *Canvas) isSelected(idx int) bool {
	for _, s := range c.Selected {
		if s == idx {
			return true
		}
	}
	return false
}

func (c *Canvas) updateSelectionFromRect() {
	x1, x2 := c.selectStart.X, c.selectEnd.X
	y1, y2 := c.selectStart.Y, c.selectEnd.Y
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	topLeft := rl.Vector2{X: x1, Y: y1}
	bottomRight := rl.Vector2{X: x2, Y: y2}

	c.Selected = nil
	for i, n := range c.Nodes {
		if n.Closing() {
			continue
		}
		if nodeIntersectsRect(n, topLeft, bottomRight) {
			c.Selected = append(c.Selected, i)
		}
	}
}

func nodeIntersectsRect(n Node, topLeft, bottomRight rl.Vector2) bool {
	pos := n.Position()
	size := n.Size()
	return pos.X+size.X >= topLeft.X && pos.X <= bottomRight.X &&
		pos.Y+size.Y >= topLeft.Y && pos.Y <= bottomRight.Y
}

func (c *Canvas) wouldOverlap(pos, size rl.Vector2) bool {
	for _, n := range c.Nodes {
		nPos := n.Position()
		nSize := n.Size()
		if pos.X+size.X > nPos.X && pos.X < nPos.X+nSize.X &&
			pos.Y+size.Y > nPos.Y && pos.Y < nPos.Y+nSize.Y {
			return true
		}
	}
	return false
}

func nodeCenter(n Node) (float64, float64) {
	pos := n.Position()
	size := n.Size()
	return float64(pos.X + size.X/2), float64(pos.Y + size.Y/2)
}

func (c *Canvas) focusDirection(dx, dy int) {
	if len(c.Nodes) == 0 {
		return
	}

	var cx, cy float64
	if c.FocusedIdx >= 0 && c.FocusedIdx < len(c.Nodes) {
		cx, cy = nodeCenter(c.Nodes[c.FocusedIdx])
	} else {
		// Use camera target as origin when nothing is focused
		cx, cy = float64(c.Camera.Target.X), float64(c.Camera.Target.Y)
	}

	bestIdx := -1
	bestScore := math.MaxFloat64

	for i, n := range c.Nodes {
		if i == c.FocusedIdx || n.Closing() {
			continue
		}
		nx, ny := nodeCenter(n)
		ddx := nx - cx
		ddy := ny - cy

		var score float64
		if c.FocusedIdx < 0 {
			// No focus: pick nearest node regardless of direction
			score = ddx*ddx + ddy*ddy
		} else {
			// Filter: candidate must be in the correct direction
			if dx < 0 && ddx >= 0 {
				continue
			}
			if dx > 0 && ddx <= 0 {
				continue
			}
			if dy < 0 && ddy >= 0 {
				continue
			}
			if dy > 0 && ddy <= 0 {
				continue
			}

			// Score with directional bias (penalize off-axis offset)
			if dx != 0 {
				score = ddx*ddx + ddy*ddy*4
			} else {
				score = ddx*ddx*4 + ddy*ddy
			}
		}

		if score < bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if bestIdx < 0 {
		return
	}

	// Unfocus old node
	if c.FocusedIdx >= 0 && c.FocusedIdx < len(c.Nodes) {
		c.Nodes[c.FocusedIdx].SetFocused(false)
	}

	// Focus new node
	c.FocusedIdx = bestIdx
	c.Nodes[bestIdx].SetFocused(true)
}

func (c *Canvas) zoomStep(dir int, mousePos rl.Vector2) {
	c.zoomMousePos = mousePos
	c.targetZoom *= (1 + float32(dir)*0.5)
	if c.targetZoom < 0.1 {
		c.targetZoom = 0.1
	}
	if c.targetZoom > 5.0 {
		c.targetZoom = 5.0
	}
}

func (c *Canvas) fillViewport(node Node) {
	gap := float32(FillViewportGap)

	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())

	// Available world-space size at current zoom
	availW := (screenW - 2*gap) / c.Camera.Zoom
	availH := (screenH - 2*gap) / c.Camera.Zoom

	cellW, cellH := node.CellSize()
	cols := uint16((int(availW) - 2*terminalPad) / cellW)
	rows := uint16((int(availH) - 2*terminalPad) / cellH)
	if cols < 2 {
		cols = 2
	}
	if rows < 2 {
		rows = 2
	}

	node.SetSize(cols, rows)

	// Smoothly animate terminal to center of viewport
	size := node.Size()
	c.nodeAnimIdx = c.FocusedIdx
	c.nodeAnimTarget = rl.Vector2{
		X: c.Camera.Target.X - size.X/2,
		Y: c.Camera.Target.Y - size.Y/2,
	}
}

func (c *Canvas) zoomToFit(node Node) {
	pos := node.Position()
	size := node.Size()

	screenW := float32(rl.GetScreenWidth())
	screenH := float32(rl.GetScreenHeight())

	// Compute zoom so the node fits with the configured pixel gap on each side
	gap := float32(ZoomToFitGap)
	zoomX := (screenW - 2*gap) / size.X
	zoomY := (screenH - 2*gap) / size.Y
	zoom := zoomX
	if zoomY < zoom {
		zoom = zoomY
	}
	if zoom < 0.1 {
		zoom = 0.1
	}
	if zoom > 5.0 {
		zoom = 5.0
	}

	// Set targets — Update() will smoothly interpolate both
	c.targetPos = rl.Vector2{
		X: pos.X + size.X/2,
		Y: pos.Y + size.Y/2,
	}
	c.animatingPos = true
	c.targetZoom = zoom
	c.zoomMousePos = rl.Vector2{X: screenW / 2, Y: screenH / 2}
}
