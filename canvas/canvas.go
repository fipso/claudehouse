package canvas

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

type Canvas struct {
	Camera     rl.Camera2D
	Nodes      []Node
	FocusedIdx int // -1 = no focus
	dragging   bool
	dragStart  rl.Vector2
	movingNode int // -1 = not moving
	moveOffset rl.Vector2

	// Selection (right-click drag)
	Selected    []int      // indices of selected nodes
	selecting   bool       // rubber-band selection active
	selectStart rl.Vector2 // world-space start of selection rect
	selectEnd   rl.Vector2 // world-space current end

	// Group move
	movingGroup    bool
	groupMoveStart rl.Vector2   // mouseWorld at drag start
	groupMoveBase  []rl.Vector2 // original positions at drag start

	// Resize
	resizing       bool
	resizeEdges    [4]bool      // top, right, bottom, left
	resizeStartPos rl.Vector2   // node position at drag start
	resizeStartW   uint16       // cols at drag start
	resizeStartH   uint16       // rows at drag start
	resizeAnchor   rl.Vector2   // mouse world at drag start

	// Smooth zoom & pan
	targetZoom   float32
	zoomMousePos rl.Vector2 // screen-space mouse pos for zoom anchoring
	targetPos    rl.Vector2 // target camera position for smooth pan
	animatingPos bool       // whether we're smoothly panning to targetPos

	// Smooth node position animation
	nodeAnimIdx    int        // index of node being animated (-1 = none)
	nodeAnimTarget rl.Vector2 // target position for the animated node
}

func NewCanvas() *Canvas {
	return &Canvas{
		Camera: rl.Camera2D{
			Offset: rl.Vector2{
				X: float32(rl.GetScreenWidth()) / 2,
				Y: float32(rl.GetScreenHeight()) / 2,
			},
			Target: rl.Vector2{X: 0, Y: 0},
			Zoom:   1.0,
		},
		FocusedIdx:  -1,
		movingNode:  -1,
		nodeAnimIdx: -1,
		targetZoom:  1.0,
	}
}

func (c *Canvas) AddNode(n Node) {
	n.StartSpawnAnim()
	c.Nodes = append(c.Nodes, n)
}

func (c *Canvas) RemoveNode(idx int) {
	if idx < 0 || idx >= len(c.Nodes) {
		return
	}
	node := c.Nodes[idx]
	if node.Closing() {
		return
	}
	node.StartCloseAnim()
	node.SetFocused(false)
	if c.FocusedIdx == idx {
		c.FocusedIdx = -1
	}
	// Remove from selection
	newSelected := c.Selected[:0]
	for _, s := range c.Selected {
		if s != idx {
			newSelected = append(newSelected, s)
		}
	}
	c.Selected = newSelected
}

func (c *Canvas) Update() {
	// Smooth pan interpolation (for zoom-to-fit)
	if c.animatingPos {
		dx := c.targetPos.X - c.Camera.Target.X
		dy := c.targetPos.Y - c.Camera.Target.Y
		if dx > -0.5 && dx < 0.5 && dy > -0.5 && dy < 0.5 {
			c.Camera.Target = c.targetPos
			c.animatingPos = false
		} else {
			c.Camera.Target.X += dx * 0.15
			c.Camera.Target.Y += dy * 0.15
		}
	}

	// Smooth node position animation
	if c.nodeAnimIdx >= 0 && c.nodeAnimIdx < len(c.Nodes) {
		n := c.Nodes[c.nodeAnimIdx]
		pos := n.Position()
		dx := c.nodeAnimTarget.X - pos.X
		dy := c.nodeAnimTarget.Y - pos.Y
		if dx > -0.5 && dx < 0.5 && dy > -0.5 && dy < 0.5 {
			n.SetPosition(c.nodeAnimTarget)
			c.nodeAnimIdx = -1
		} else {
			n.SetPosition(rl.Vector2{
				X: pos.X + dx*0.15,
				Y: pos.Y + dy*0.15,
			})
		}
	}

	// Smooth zoom interpolation
	diff := c.targetZoom - c.Camera.Zoom
	if diff != 0 {
		if !c.animatingPos {
			// Only anchor to mouse when not doing a zoom-to-fit pan
			mouseWorldBefore := rl.GetScreenToWorld2D(c.zoomMousePos, c.Camera)
			if diff > -0.001 && diff < 0.001 {
				c.Camera.Zoom = c.targetZoom
			} else {
				c.Camera.Zoom += diff * 0.15
			}
			mouseWorldAfter := rl.GetScreenToWorld2D(c.zoomMousePos, c.Camera)
			c.Camera.Target.X += mouseWorldBefore.X - mouseWorldAfter.X
			c.Camera.Target.Y += mouseWorldBefore.Y - mouseWorldAfter.Y
		} else {
			if diff > -0.001 && diff < 0.001 {
				c.Camera.Zoom = c.targetZoom
			} else {
				c.Camera.Zoom += diff * 0.15
			}
		}
	}

	for _, n := range c.Nodes {
		n.Update()
	}

	// Sweep nodes whose close animation is done
	for i := len(c.Nodes) - 1; i >= 0; i-- {
		if c.Nodes[i].AnimDone() {
			c.Nodes[i].Free()
			c.Nodes = append(c.Nodes[:i], c.Nodes[i+1:]...)
			// Adjust FocusedIdx
			if c.FocusedIdx == i {
				c.FocusedIdx = -1
			} else if c.FocusedIdx > i {
				c.FocusedIdx--
			}
			// Adjust Selected indices
			newSelected := c.Selected[:0]
			for _, s := range c.Selected {
				if s == i {
					continue
				}
				if s > i {
					s--
				}
				newSelected = append(newSelected, s)
			}
			c.Selected = newSelected
		}
	}
}

func (c *Canvas) Draw() {
	rl.BeginMode2D(c.Camera)

	// Draw grid dots
	c.drawGrid()

	// Draw all nodes
	for _, n := range c.Nodes {
		n.Draw(c.Camera)
	}

	// Draw selected node borders (yellow/orange)
	for _, idx := range c.Selected {
		if idx >= 0 && idx < len(c.Nodes) {
			n := c.Nodes[idx]
			pos := n.Position()
			size := n.Size()
			rl.DrawRectangleLines(
				int32(pos.X)-2, int32(pos.Y)-2,
				int32(size.X)+4, int32(size.Y)+4,
				rl.Color{R: 255, G: 180, B: 0, A: 255},
			)
		}
	}

	// Draw rubber-band selection rectangle
	if c.selecting {
		x1, x2 := c.selectStart.X, c.selectEnd.X
		y1, y2 := c.selectStart.Y, c.selectEnd.Y
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		// Semi-transparent fill
		rl.DrawRectangle(int32(x1), int32(y1), int32(x2-x1), int32(y2-y1),
			rl.Color{R: 100, G: 150, B: 255, A: 50})
		// Solid border
		rl.DrawRectangleLines(int32(x1), int32(y1), int32(x2-x1), int32(y2-y1),
			rl.Color{R: 100, G: 150, B: 255, A: 255})
	}

	rl.EndMode2D()

	// HUD (drawn in screen space)
	c.drawHUD()
}

func (c *Canvas) drawGrid() {
	// Calculate visible area in world space
	topLeft := rl.GetScreenToWorld2D(rl.Vector2{X: 0, Y: 0}, c.Camera)
	bottomRight := rl.GetScreenToWorld2D(
		rl.Vector2{X: float32(rl.GetScreenWidth()), Y: float32(rl.GetScreenHeight())},
		c.Camera)

	startX := float32(int(topLeft.X/gridSize)) * gridSize
	startY := float32(int(topLeft.Y/gridSize)) * gridSize

	dotColor := rl.Color{R: 60, G: 60, B: 60, A: 255}
	dotSize := float32(2.0) / c.Camera.Zoom
	if dotSize < 1 {
		dotSize = 1
	}

	for x := startX; x < bottomRight.X; x += gridSize {
		for y := startY; y < bottomRight.Y; y += gridSize {
			rl.DrawCircleV(rl.Vector2{X: x, Y: y}, float32(dotSize), dotColor)
		}
	}
}

func (c *Canvas) drawHUD() {
	screenH := int32(rl.GetScreenHeight())

	// Show zoom level
	zoomText := fmt.Sprintf("%.0f%%", c.Camera.Zoom*100)
	rl.DrawText(zoomText, 10, screenH-25, 16,
		rl.Color{R: 150, G: 150, B: 150, A: 200})

	// Keybind bar
	var hints string
	if c.FocusedIdx >= 0 && c.FocusedIdx < len(c.Nodes) {
		hints = "2×Esc unfocus  Alt+HJKL navigate  Alt+E zoom-fit  Alt+F fill  Alt+Enter new  Alt+Q close  drag edge resize"
	} else {
		hints = "Double-click new terminal  Click focus  Alt+HJKL navigate  Alt+Click move  Right-click select  Scroll zoom"
	}
	textW := rl.MeasureText(hints, 14)
	screenW := int32(rl.GetScreenWidth())
	rl.DrawText(hints, (screenW-textW)/2, screenH-20, 14,
		rl.Color{R: 120, G: 120, B: 120, A: 180})
}

func (c *Canvas) FreeAll() {
	for _, n := range c.Nodes {
		n.Free()
	}
	c.Nodes = nil
}
