package terminal

import (
	"claudehouse/ghostty"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func raylibKeyToGhostty(rlKey int32) ghostty.Key {
	// Letters — contiguous range
	if rlKey >= rl.KeyA && rlKey <= rl.KeyZ {
		return ghostty.KeyA + ghostty.Key(rlKey-rl.KeyA)
	}
	// Digits — contiguous range
	if rlKey >= rl.KeyZero && rlKey <= rl.KeyNine {
		return ghostty.KeyDigit0 + ghostty.Key(rlKey-rl.KeyZero)
	}
	// F-keys — contiguous range
	if rlKey >= rl.KeyF1 && rlKey <= rl.KeyF12 {
		return ghostty.KeyF1 + ghostty.Key(rlKey-rl.KeyF1)
	}

	switch rlKey {
	case rl.KeySpace:
		return ghostty.KeySpace
	case rl.KeyEnter:
		return ghostty.KeyEnter
	case rl.KeyTab:
		return ghostty.KeyTab
	case rl.KeyBackspace:
		return ghostty.KeyBackspace
	case rl.KeyDelete:
		return ghostty.KeyDelete
	case rl.KeyEscape:
		return ghostty.KeyEscape
	case rl.KeyUp:
		return ghostty.KeyArrowUp
	case rl.KeyDown:
		return ghostty.KeyArrowDown
	case rl.KeyLeft:
		return ghostty.KeyArrowLeft
	case rl.KeyRight:
		return ghostty.KeyArrowRight
	case rl.KeyHome:
		return ghostty.KeyHome
	case rl.KeyEnd:
		return ghostty.KeyEnd
	case rl.KeyPageUp:
		return ghostty.KeyPageUp
	case rl.KeyPageDown:
		return ghostty.KeyPageDown
	case rl.KeyInsert:
		return ghostty.KeyInsert
	case rl.KeyMinus:
		return ghostty.KeyMinus
	case rl.KeyEqual:
		return ghostty.KeyEqual
	case rl.KeyLeftBracket:
		return ghostty.KeyBracketLeft
	case rl.KeyRightBracket:
		return ghostty.KeyBracketRight
	case rl.KeyBackSlash:
		return ghostty.KeyBackslash
	case rl.KeySemicolon:
		return ghostty.KeySemicolon
	case rl.KeyApostrophe:
		return ghostty.KeyQuote
	case rl.KeyComma:
		return ghostty.KeyComma
	case rl.KeyPeriod:
		return ghostty.KeyPeriod
	case rl.KeySlash:
		return ghostty.KeySlash
	case rl.KeyGrave:
		return ghostty.KeyBackquote
	default:
		return ghostty.KeyUnidentified
	}
}

func getGhosttyMods() ghostty.Mods {
	var mods ghostty.Mods
	if rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift) {
		mods |= ghostty.ModsShift
	}
	if rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl) {
		mods |= ghostty.ModsCtrl
	}
	if rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt) {
		mods |= ghostty.ModsAlt
	}
	if rl.IsKeyDown(rl.KeyLeftSuper) || rl.IsKeyDown(rl.KeyRightSuper) {
		mods |= ghostty.ModsSuper
	}
	return mods
}

func unshiftedCodepoint(rlKey int32) uint32 {
	if rlKey >= rl.KeyA && rlKey <= rl.KeyZ {
		return uint32('a') + uint32(rlKey-rl.KeyA)
	}
	if rlKey >= rl.KeyZero && rlKey <= rl.KeyNine {
		return uint32('0') + uint32(rlKey-rl.KeyZero)
	}

	switch rlKey {
	case rl.KeySpace:
		return ' '
	case rl.KeyMinus:
		return '-'
	case rl.KeyEqual:
		return '='
	case rl.KeyLeftBracket:
		return '['
	case rl.KeyRightBracket:
		return ']'
	case rl.KeyBackSlash:
		return '\\'
	case rl.KeySemicolon:
		return ';'
	case rl.KeyApostrophe:
		return '\''
	case rl.KeyComma:
		return ','
	case rl.KeyPeriod:
		return '.'
	case rl.KeySlash:
		return '/'
	case rl.KeyGrave:
		return '`'
	default:
		return 0
	}
}

func raylibMouseToGhostty(rlBtn rl.MouseButton) ghostty.MouseButton {
	switch rlBtn {
	case rl.MouseButtonLeft:
		return ghostty.MouseButtonLeft
	case rl.MouseButtonRight:
		return ghostty.MouseButtonRight
	case rl.MouseButtonMiddle:
		return ghostty.MouseButtonMiddle
	case rl.MouseButtonSide:
		return ghostty.MouseButtonFour
	case rl.MouseButtonExtra:
		return ghostty.MouseButtonFive
	case rl.MouseButtonForward:
		return ghostty.MouseButtonSix
	case rl.MouseButtonBack:
		return ghostty.MouseButtonSeven
	default:
		return ghostty.MouseButtonUnknown
	}
}
