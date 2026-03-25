package canvas

import rl "github.com/gen2brain/raylib-go/raylib"

type Node interface {
	Update()
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
