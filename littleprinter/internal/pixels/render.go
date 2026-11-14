// Rendering: images and text to 1-bit pixel buffers (384 wide).
package pixels

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"

	_ "image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
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

// FromText renders plain text to pixels: wrapped to the paper width,
// 7x13 bitmap font, white background.
func FromText(text string) (pix []bool, width int, err error) {
	face := basicfont.Face7x13
	const charW = 7
	const lineH = 18
	const maxChars = PaperWidth / charW

	// Word-wrap.
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(para)
		line := ""
		for _, w := range words {
			candidate := w
			if line != "" {
				candidate = line + " " + w
			}
			if len(candidate) <= maxChars {
				line = candidate
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			// Hard-break overlong words.
			for len(w) > maxChars {
				lines = append(lines, w[:maxChars])
				w = w[maxChars:]
			}
			line = w
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

	d := &font.Drawer{
		Dst:  dst,
		Src:  image.Black,
		Face: face,
	}
	for i, line := range lines {
		d.Dot = fixed.P(0, i*lineH+13)
		d.DrawString(line)
	}

	// Round-trip through the image path; dithering of bilevel content
	// is lossless.
	return FromImage(encodePNG(dst))
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // cannot fail for in-memory RGBA
	}
	return buf.Bytes()
}
