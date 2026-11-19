package sudoku

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Layout constants for the 384px paper.
const (
	paperW     = 384
	marginX    = 12
	headerH    = 36
	cell       = 40           // px per cell → grid is 360 wide
	thinLine   = 2            // every-cell lines
	thickLine  = 4            // 3x3 separators + border
	digitSize  = 28
	headerSize = 20
	footerH    = 30
)

// RenderPNG draws the puzzle (0 = blank) to a PNG, 384px wide, with a
// header (title/date/puzzle no.) and footer (difficulty). Solution
// printing just passes a full grid plus showSolution styling.
func RenderPNG(g *Grid, date string, difficulty string, solution bool) []byte {
	gridH := 9*cell + footerH
	h := headerH + gridH
	dst := image.NewRGBA(image.Rect(0, 0, paperW, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)

	title := "SUDOKU"
	if solution {
		title += " — SOLUTION"
	} else {
		title += " · " + difficulty
	}
	drawText(dst, title, headerSize, marginX, 4, false)
	// Right-aligned puzzle number on the same line.
	info := fmt.Sprintf("no. %d", PuzzleNumber(date))
	w := textWidth(info, headerSize)
	drawText(dst, info, headerSize, paperW-marginX-w, 4, false)

	gx0, gy0 := marginX, headerH

	// Lines: thin everywhere, thick at box boundaries and the border.
	line := func(pos int, thick bool) {
		// pos in 0..9 (cell index boundary)
		x0 := gx0 + pos*cell
		y0 := gy0
		t := thinLine
		if thick {
			t = thickLine
		}
		// Vertical line, shifted so thick lines stay inside the grid.
		off := t / 2
		if pos == 0 {
			off = 0
		} else if pos == 9 {
			off = t
		}
		if x0-off >= 0 {
			draw.Draw(dst, image.Rect(x0-off, y0, x0-off+t, y0+9*cell), image.Black, image.Point{}, draw.Src)
		}
		// Horizontal line
		y := gy0 + pos*cell
		if y-off >= 0 {
			draw.Draw(dst, image.Rect(gx0, y-off, gx0+9*cell, y-off+t), image.Black, image.Point{}, draw.Src)
		}
	}
	for i := 0; i <= 9; i++ {
		line(i, i%3 == 0)
	}

	// Digits.
	f := digitFace(digitSize)
	d := &font.Drawer{Face: f, Dst: dst, Src: image.Black}
	for i, v := range g {
		if v == 0 {
			continue
		}
		row, col := i/9, i%9
		cx, cy := gx0+col*cell, gy0+row*cell
		adv, _ := f.GlyphAdvance(rune('0' + v))
		dw := adv.Ceil()
		m := f.Metrics()
		d.Dot = fixed.P(cx+(cell-dw)/2, cy+(cell+int(digitSize))/2-int(m.Descent.Ceil()/2))
		d.DrawString(string(rune('0' + v)))
	}

	// Footer: date + (for puzzles) the no-peeking note.
	foot := fmt.Sprintf("%s — One unique solution. No peeking.", date)
	if solution {
		foot = fmt.Sprintf("%s — Told you it was solvable.", date)
	}
	drawText(dst, foot, 14, marginX, headerH+9*cell+6, false)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		panic(err) // cannot fail for in-memory RGBA
	}
	return buf.Bytes()
}

var (
	fontOnce sync.Once
	fontObj  *opentype.Font
	fontErr  error
)

func loadFont() {
	fontOnce.Do(func() {
		fontObj, fontErr = opentype.Parse(goregular.TTF)
	})
}

func digitFace(size float64) font.Face {
	loadFont()
	if fontErr != nil {
		panic(fontErr)
	}
	f, err := opentype.NewFace(fontObj, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	return f
}

func textWidth(s string, size float64) int {
	f := digitFace(size)
	w := 0
	for _, r := range s {
		adv, ok := f.GlyphAdvance(r)
		if !ok {
			continue
		}
		w += adv.Ceil()
	}
	return w
}

// drawText draws a text run at the given top-left position.
func drawText(dst *image.RGBA, s string, size float64, x, y int, bold bool) {
	f := digitFace(size)
	d := &font.Drawer{Face: f, Dst: dst, Src: image.Black}
	m := f.Metrics()
	d.Dot = fixed.P(x, y+int(size)+m.Descent.Ceil()/2)
	d.DrawString(s)
}
