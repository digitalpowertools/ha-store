// Rendering: images and text to 1-bit pixel buffers (384 wide).
package pixels

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"sync"

	_ "image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// PaperWidth is the Little Printer's print head width in pixels.
const PaperWidth = 384

const maxHeight = 10000

// FromImage decodes a PNG/JPEG, crops to PaperWidth, and converts to
// 1-bit pixels (true = black). Transparent pixels are white. Uses
// Atkinson dithering, which looks great on thermal paper.
func FromImage(data []byte) (pix []bool, width int, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("decode image: %w", err)
	}

	w := img.Bounds().Dx()
	if w > PaperWidth {
		w = PaperWidth
	}
	h := img.Bounds().Dy()
	if h > maxHeight {
		h = maxHeight
	}

	// Luminance buffer.
	gray := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			// Fully transparent pixels are white.
			if a>>8 == 0 {
				gray[y*w+x] = 255
				continue
			}
			gray[y*w+x] = 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
		}
	}

	// Atkinson dithering: error diffused at 1/8 to six neighbours.
	pix = make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			old := gray[y*w+x]
			black := old < 128
			pix[y*w+x] = black
			var errd float64
			if black {
				errd = old // error = old - 0
			} else {
				errd = old - 255
			}
			diffuse := func(dx, dy int) {
				nx, ny := x+dx, y+dy
				if nx < 0 || nx >= w || ny < 0 || ny >= h {
					return
				}
				gray[ny*w+nx] += errd / 8
			}
			diffuse(1, 0)
			diffuse(2, 0)
			diffuse(-1, 1)
			diffuse(0, 1)
			diffuse(1, 1)
			diffuse(0, 2)
		}
	}
	return pix, w, nil
}

// FromText renders plain text to pixels: word-wrapped to the paper
// width, white background.
//
// Size and typography match the original sirius server's default print
// template (sirius/coding/default_template.html): Helvetica/Arial at
// font-size 30px on the 384px-wide page, rendered headless and dithered.
// We embed Go Regular (metrically similar to Helvetica) via
// golang.org/x/image/font/gofont, so no font files are needed.
func FromText(text string) (pix []bool, width int, err error) {
	return FromTextSize(text, 0)
}

// DefaultTextSize matches sirius's template body font-size (30px).
const DefaultTextSize = 30

// MaxTextSize caps the configurable size; above ~100px a single word
// barely fits the paper width.
const MaxTextSize = 100

// TextMargin is the left/right padding in pixels (sirius's template
// used 0, but a little breathing room reads better and avoids the
// printhead's edge artifacts).
const TextMargin = 8

// FromTextSize renders text at an explicit pixel size; size <= 0
// selects DefaultTextSize.
func FromTextSize(text string, size float64) (pix []bool, width int, err error) {
	img, err := renderTextImage(text, size, PaperWidth-2*TextMargin)
	if err != nil {
		return nil, 0, err
	}
	// Round-trip through the image path; dithering of bilevel content
	// is lossless.
	return FromImage(encodePNG(img))
}

// FromTextBubble renders text inside a speech balloon (BERG-app style):
// a bordered bubble with a tail pointing down at the left. The protocol
// doesn't know about balloons — this is pure rasterization.
func FromTextBubble(text string, size float64) (pix []bool, width int, err error) {
	if size <= 0 {
		size = DefaultTextSize
	}

	const (
		border   = 3  // bubble outline thickness
		pad      = 12 // text inset from the border
		balloonM = 8  // bubble inset from the paper edge
		tailH    = 26 // tail height
		topPad   = 6  // paper margin above the bubble
		botPad   = 4  // paper margin below the tail
	)
	innerW := PaperWidth - 2*balloonM - 2*border - 2*pad

	textImg, err := renderTextImage(text, size, innerW)
	if err != nil {
		return nil, 0, err
	}
	// Crop to the actual ink: blitting the full canvas would paint its
	// white margins over the bubble border.
	bbox := contentBBox(textImg)
	tw, th := bbox.Dx(), bbox.Dy()

	height := topPad + 2*border + 2*pad + th + border + tailH + botPad
	if height > maxHeight {
		return nil, 0, errors.New("text too long")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, PaperWidth, height))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)

	bx0, by0 := balloonM, topPad
	bx1, by1 := PaperWidth-1-balloonM, topPad+2*border+2*pad+th
	rect := image.Rect(bx0, by0, bx1+1, by1+1)

	// Bubble: filled black rect, white inner rect (the border), text,
	// then shave the corners for a rounded look.
	fillRect(canvas, rect, image.Black)
	fillRect(canvas, rect.Inset(border), image.White)
	draw.Draw(canvas,
		image.Rect(bx0+border+pad, by0+border+pad, bx0+border+pad+tw, by0+border+pad+th),
		textImg, bbox.Min, draw.Src)
	for _, c := range []image.Point{
		{bx0, by0}, {bx1, by0}, {bx0, by1}, {bx1, by1},
	} {
		fillRect(canvas, image.Rect(c.X-3, c.Y-3, c.X+4, c.Y+4), image.White)
	}

	// Tail: a black triangle with a white triangle inside, hanging off
	// the bubble's bottom edge near the left.
	outer := [3]image.Point{
		{bx0 + 24, by1},
		{bx0 + 64, by1},
		{bx0 + 30, by1 + tailH},
	}
	fillTriangle(canvas, outer, image.Black)
	fillTriangle(canvas, insetTriangle(outer, 0.12), image.White)

	// Round-trip through the image path; dithering of bilevel content
	// is lossless.
	return FromImage(encodePNG(canvas))
}

// contentBBox returns the smallest rectangle containing all non-white
// pixels of img.
func contentBBox(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || bl != 0xffff {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

// renderTextImage draws wrapped text on a white canvas exactly
// maxW pixels wide.
func renderTextImage(text string, size float64, maxW int) (*image.RGBA, error) {
	if size <= 0 || size > MaxTextSize {
		size = DefaultTextSize
	}
	face, err := newTextFace(size)
	if err != nil {
		return nil, fmt.Errorf("text font: %w", err)
	}
	d := &font.Drawer{Face: face}
	advance := func(s string) int { return d.MeasureString(s).Ceil() }

	// CSS default line-height is ~1.2; round up a touch for the paper.
	lineH := int(size*1.25) + 1

	// Word-wrap by measured width.
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, w := range strings.Fields(para) {
			candidate := w
			if line != "" {
				candidate = line + " " + w
			}
			if advance(candidate) <= maxW {
				line = candidate
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			// Hard-break overlong words rune-wise.
			line = ""
			for _, r := range w {
				if advance(line+string(r)) > maxW {
					lines = append(lines, line)
					line = string(r)
				} else {
					line += string(r)
				}
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}

	h := len(lines) * lineH
	if h > maxHeight {
		return nil, errors.New("text too long")
	}

	dst := image.NewRGBA(image.Rect(0, 0, PaperWidth, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)

	d.Dst, d.Src = dst, image.Black
	for i, line := range lines {
		d.Dot = fixed.P(TextMargin, i*lineH+int(size))
		d.DrawString(line)
	}
	return dst, nil
}

// fillRect paints a rectangle with the given color.
func fillRect(dst *image.RGBA, r image.Rectangle, c *image.Uniform) {
	draw.Draw(dst, r, c, image.Point{}, draw.Src)
}

// fillTriangle scanline-fills a triangle using barycentric coordinates.
func fillTriangle(dst *image.RGBA, pts [3]image.Point, c *image.Uniform) {
	minX := min3(pts[0].X, pts[1].X, pts[2].X)
	maxX := max3(pts[0].X, pts[1].X, pts[2].X)
	minY := min3(pts[0].Y, pts[1].Y, pts[2].Y)
	maxY := max3(pts[0].Y, pts[1].Y, pts[2].Y)

	denom := (pts[1].Y-pts[2].Y)*(pts[0].X-pts[2].X) +
		(pts[2].X-pts[1].X)*(pts[0].Y-pts[2].Y)
	if denom == 0 {
		return
	}

	for y := minY; y <= maxY; y++ {
		rowStart, rowEnd := -1, -1
		for x := minX; x <= maxX; x++ {
			w0 := ((pts[1].Y-pts[2].Y)*(x-pts[2].X) + (pts[2].X-pts[1].X)*(y-pts[2].Y))
			w1 := ((pts[2].Y-pts[0].Y)*(x-pts[2].X) + (pts[0].X-pts[2].X)*(y-pts[2].Y))
			w2 := denom - w0 - w1
			if w0 >= 0 && w1 >= 0 && w2 >= 0 {
				if rowStart < 0 {
					rowStart = x
				}
				rowEnd = x
			}
		}
		if rowStart >= 0 {
			fillRect(dst, image.Rect(rowStart, y, rowEnd+1, y+1), c)
		}
	}
}

// insetTriangle moves each vertex toward the centroid by the given
// fraction of the triangle's size, producing a concentric inner shape.
func insetTriangle(pts [3]image.Point, frac float64) [3]image.Point {
	cx := (pts[0].X + pts[1].X + pts[2].X) / 3
	cy := (pts[0].Y + pts[1].Y + pts[2].Y) / 3
	var out [3]image.Point
	for i, p := range pts {
		out[i] = image.Point{
			X: cx + int(float64(p.X-cx)*(1-frac)),
			Y: cy + int(float64(p.Y-cy)*(1-frac)),
		}
	}
	return out
}

func min3(a, b, c int) int { return min(min(a, b), c) }
func max3(a, b, c int) int { return max(max(a, b), c) }

var (
	textFontOnce sync.Once
	textFont     *opentype.Font
	textFontErr  error
)

// newTextFace returns the embedded Go Regular at the given pixel size.
func newTextFace(size float64) (font.Face, error) {
	textFontOnce.Do(func() {
		textFont, textFontErr = opentype.Parse(goregular.TTF)
	})
	if textFontErr != nil {
		return nil, textFontErr
	}
	return opentype.NewFace(textFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // cannot fail for in-memory RGBA
	}
	return buf.Bytes()
}
