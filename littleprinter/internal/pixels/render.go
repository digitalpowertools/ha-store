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
	if size <= 0 || size > MaxTextSize {
		size = DefaultTextSize
	}
	face, err := newTextFace(size)
	if err != nil {
		return nil, 0, fmt.Errorf("text font: %w", err)
	}
	d := &font.Drawer{Face: face}
	advance := func(s string) int { return d.MeasureString(s).Ceil() }

	maxW := PaperWidth - 2*TextMargin
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
				if advance(line + string(r)) > maxW {
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
		return nil, 0, errors.New("text too long")
	}

	dst := image.NewRGBA(image.Rect(0, 0, PaperWidth, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)

	d.Dst, d.Src = dst, image.Black
	for i, line := range lines {
		d.Dot = fixed.P(TextMargin, i*lineH+int(size))
		d.DrawString(line)
	}

	// Round-trip through the image path; dithering of bilevel content
	// is lossless.
	return FromImage(encodePNG(dst))
}

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
