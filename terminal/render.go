package terminal

import (
	"claudehouse/ghostty"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func utf8Encode(cp uint32, buf []byte) int {
	const maxUnicode = 0x10FFFF
	const replacementChar = 0xFFFD

	if cp > maxUnicode {
		cp = replacementChar
	}

	if cp < 0x80 {
		buf[0] = byte(cp)
		return 1
	} else if cp < 0x800 {
		buf[0] = byte(0xC0 | (cp >> 6))
		buf[1] = byte(0x80 | (cp & 0x3F))
		return 2
	} else if cp < 0x10000 {
		buf[0] = byte(0xE0 | (cp >> 12))
		buf[1] = byte(0x80 | ((cp >> 6) & 0x3F))
		buf[2] = byte(0x80 | (cp & 0x3F))
		return 3
	} else {
		buf[0] = byte(0xF0 | (cp >> 18))
		buf[1] = byte(0x80 | ((cp >> 12) & 0x3F))
		buf[2] = byte(0x80 | ((cp >> 6) & 0x3F))
		buf[3] = byte(0x80 | (cp & 0x3F))
		return 4
	}
}

func DrawTerminal(rs *ghostty.RenderState, ri *ghostty.RowIterator, rc *ghostty.RowCells,
	font rl.Font, cellW, cellH, fontSize, padX, padY int, alpha uint8) {

	colors := rs.GetColors()
	defaultFg := colors.Foreground
	defaultBg := colors.Background

	if !ri.Init(rs) {
		return
	}

	y := padY

	var cpBuf [16]uint32
	var textBuf [64]byte

	for ri.Next() {
		if !ri.GetCells(rc) {
			continue
		}

		x := padX

		for rc.Next() {
			graphemeLen := rc.GraphemeLen()

			if graphemeLen == 0 {
				// Empty cell — may have bg color
				if bg, ok := rc.BgColor(); ok {
					rl.DrawRectangle(int32(x), int32(y), int32(cellW), int32(cellH),
						rl.Color{R: bg.R, G: bg.G, B: bg.B, A: alpha})
				}
				x += cellW
				continue
			}

			// Read grapheme codepoints
			length := int(graphemeLen)
			if length > 16 {
				length = 16
			}
			rc.Graphemes(cpBuf[:length])

			// Build UTF-8 string
			pos := 0
			for i := 0; i < length && pos < 60; i++ {
				n := utf8Encode(cpBuf[i], textBuf[pos:])
				pos += n
			}
			textBuf[pos] = 0 // null terminate

			// Resolve colors
			fg := defaultFg
			if fgColor, ok := rc.FgColor(); ok {
				fg = fgColor
			}
			bgRgb := defaultBg
			hasBg := false
			if bgColor, ok := rc.BgColor(); ok {
				bgRgb = bgColor
				hasBg = true
			}

			// Style
			style := rc.GetStyle()

			// Inverse: swap fg/bg
			if style.Inverse {
				fg, bgRgb = bgRgb, fg
				hasBg = true
			}

			rayFg := rl.Color{R: fg.R, G: fg.G, B: fg.B, A: alpha}

			// Draw background
			if hasBg {
				rl.DrawRectangle(int32(x), int32(y), int32(cellW), int32(cellH),
					rl.Color{R: bgRgb.R, G: bgRgb.G, B: bgRgb.B, A: alpha})
			}

			// Italic offset
			italicOffset := 0
			if style.Italic {
				italicOffset = fontSize / 6
			}

			text := string(textBuf[:pos])
			rl.DrawTextEx(font, text, rl.Vector2{X: float32(x + italicOffset), Y: float32(y)},
				float32(fontSize), 0, rayFg)

			// Bold: double-strike
			if style.Bold {
				rl.DrawTextEx(font, text, rl.Vector2{X: float32(x + italicOffset + 1), Y: float32(y)},
					float32(fontSize), 0, rayFg)
			}

			x += cellW
		}

		ri.SetClean()
		y += cellH
	}

	// Draw cursor
	if rs.GetCursorVisible() && rs.GetCursorInViewport() {
		cx, cy := rs.GetCursorPos()
		curColor := colors.Foreground
		if colors.CursorHasValue {
			curColor = colors.Cursor
		}
		curX := padX + int(cx)*cellW
		curY := padY + int(cy)*cellH
		curAlpha := uint8(128)
		if alpha < 255 {
			curAlpha = uint8(uint16(curAlpha) * uint16(alpha) / 255)
		}
		rl.DrawRectangle(int32(curX), int32(curY), int32(cellW), int32(cellH),
			rl.Color{R: curColor.R, G: curColor.G, B: curColor.B, A: curAlpha})
	}

	rs.SetClean()
}
