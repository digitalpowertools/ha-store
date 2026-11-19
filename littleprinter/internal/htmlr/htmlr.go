// Package htmlr renders a pragmatic subset of HTML directly to the
// Little Printer's 1-bit pixel buffer — no PNG intermediate, no
// headless browser.
//
// The output target is 384px-wide thermal paper, so the layout model
// is deliberately tiny: a vertical stack of blocks (headings,
// paragraphs, lists, rules) whose inline content supports bold,
// italic, monospace and monochrome emoji. See README for the exact
// subset. Unsupported CSS is ignored; unsupported elements are treated
// as transparent containers.
package htmlr

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strconv"
	"strings"

	"littleprinter/internal/pixels"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/net/html"
)

// PaperWidth matches pixels.PaperWidth (kept independent to avoid an
// import cycle; the API layer passes results through pixels.Encode).
const PaperWidth = 384

const (
	maxHeight = 10000
	// PageMargin is the left/right page margin in pixels.
	PageMargin = 10
	// DefaultSize is the body font size in px. HTML documents carry
	// more content than plain-text prints, so this is denser than
	// pixels.DefaultTextSize.
	DefaultSize = 24
	lineHeight  = 1.3
)

// style is the resolved typography of a run of text.
type style struct {
	bold, italic, mono bool
}

// word is a single whitespace-delimited token. spaceBefore records
// whether whitespace preceded the token in the source, so glued text
// like foo<b>bar</b> wraps without an injected space.
type word struct {
	text        string
	st          style
	spaceBefore bool
	br          bool // explicit <br/>
}

// block is a vertical layout unit.
type block struct {
	words       []word
	size        float64
	align       int // 0 left, 1 center, 2 right
	marker      string // list bullet / number, drawn in the left gutter
	rule        bool   // <hr>
	spaceBefore int
	spaceAfter  int
}

type builder struct {
	blocks   []block
	cur      *block
	pending  bool // whitespace pending before the next word
	listKind []string // stack of open list kinds ("ul"/"ol")
	listN    []int    // ol counters parallel to listKind
}

func (b *builder) flush() {
	if b.cur != nil && (len(b.cur.words) > 0 || b.cur.rule) {
		b.blocks = append(b.blocks, *b.cur)
	}
	b.cur = nil
	b.pending = false
}

// start opens a new block with the given base style and spacing.
func (b *builder) start(size float64, before, after int) {
	b.flush()
	b.cur = &block{size: size, spaceBefore: before, spaceAfter: after}
}

// addText appends inline text in the given style, collapsing
// whitespace and recording word boundaries.
func (b *builder) addText(s string, st style) {
	if b.cur == nil {
		b.cur = &block{size: DefaultSize}
	}
	s = strings.ReplaceAll(s, "\u200d", "") // ZWJ: sequences draw as components
	s = strings.TrimFunc(s, func(r rune) bool {
		return r >= 0xFE00 && r <= 0xFE0F || // variation selectors
			r >= 0x1F3FB && r <= 0x1F3FF // skin tone modifiers
	})
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	for i, tok := range strings.Split(s, " ") {
		if tok == "" {
			if i > 0 || b.pending {
				b.pending = true
			}
			continue
		}
		b.cur.words = append(b.cur.words, word{text: tok, st: st, spaceBefore: b.pending})
		b.pending = true
	}
}

// isEmoji reports whether r should be rendered with the emoji face.
func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // emoji proper + supplement
	case r >= 0x2600 && r <= 0x27BF: // misc symbols, dingbats
	case r >= 0x2B00 && r <= 0x2BFF: // stars, arrows
	case r == 0x231A || r == 0x231B: // watch, hourglass
	case r >= 0x23E9 && r <= 0x23FA: // media controls, clock
	case r >= 0x2190 && r <= 0x21FF && false: // arrows stay text
	default:
		return false
	}
	return true
}

// FromHTML parses HTML and renders it to 1-bit pixels (true = black),
// cropped to the paper width.
func FromHTML(body []byte) (pix []bool, width int, err error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("parse html: %w", err)
	}

	b := &builder{}
	b.walk(root, style{})
	b.flush()

	img, err := b.render()
	if err != nil {
		return nil, 0, err
	}
	return toPixels(img)
}

// skipElements produce no output (metadata or raw scripts).
var skipElements = map[string]bool{
	"head": true, "script": true, "style": true, "title": true,
	"template": true, "meta": true, "link": true, "base": true,
}

// blockSizes defines the scale factor and vertical margins per block
// element, as multiples of the default size.
var blockSizes = map[string]struct {
	scale      float64
	before, after int
}{
	"h1": {2.0, 16, 8},
	"h2": {1.55, 14, 6},
	"h3": {1.3, 12, 5},
	"h4": {1.15, 10, 4},
	"p":  {1.0, 0, 9},
	"li": {1.0, 0, 3},
}

// walk descends the parsed tree, emitting blocks and styled text.
func (b *builder) walk(n *html.Node, st style) {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "br":
			if b.cur == nil {
				b.cur = &block{size: DefaultSize}
			}
			b.cur.words = append(b.cur.words, word{br: true})
			b.pending = false
			return
		case "hr":
			b.flush()
			b.cur = &block{rule: true, size: DefaultSize, spaceBefore: 10, spaceAfter: 10}
			return
		case "ul", "ol":
			b.flush()
			b.listKind = append(b.listKind, n.Data)
			b.listN = append(b.listN, 0)
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				b.walk(c, st)
			}
			b.listKind = b.listKind[:len(b.listKind)-1]
			b.listN = b.listN[:len(b.listN)-1]
			return
		case "li":
			if b.cur != nil {
				b.flush()
			}
			depth := len(b.listKind)
			size, before, after := blockSizes["p"].scale*DefaultSize, 0, blockSizes["li"].after
			b.cur = &block{size: size, spaceBefore: before, spaceAfter: after}
			if depth > 0 {
				switch b.listKind[depth-1] {
				case "ol":
					b.listN[depth-1]++
					b.cur.marker = strconv.Itoa(b.listN[depth-1]) + "."
				default:
					b.cur.marker = "•"
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				b.walk(c, st)
			}
			b.flush()
			return
		}

		if skipElements[n.Data] {
			return
		}

		// Block-level open: resolve heading/para styling.
		if def, ok := blockSizes[n.Data]; ok {
			size := def.scale * DefaultSize
			st = style{bold: strings.HasPrefix(n.Data, "h")}
			b.start(size, def.before, def.after)
		} else if n.Data == "b" || n.Data == "strong" {
			st.bold = true
		} else if n.Data == "i" || n.Data == "em" {
			st.italic = true
		} else if n.Data == "code" || n.Data == "tt" {
			st.mono = true
		}

		// Inline style attribute (Phase 1: text-align, font-size).
		if b.cur != nil {
			for _, attr := range n.Attr {
				if attr.Key == "style" {
					applyInlineStyle(b.cur, attr.Val)
				}
			}
		}
	}

	if n.Type == html.TextNode {
		b.addText(n.Data, st)
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c, st)
	}
}

// applyInlineStyle parses a CSS style attribute for the supported
// properties, mutating blk in place.
func applyInlineStyle(blk *block, css string) {
	for _, decl := range strings.Split(css, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(decl), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(strings.ToLower(k)), strings.TrimSpace(strings.ToLower(v))
		switch k {
		case "text-align":
			switch v {
			case "center":
				blk.align = 1
			case "right":
				blk.align = 2
			}
		case "font-size":
			v = strings.TrimSuffix(v, "px")
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 4 && n <= 100 {
				blk.size = float64(n)
			}
		}
	}
}

// faceForRune picks the emoji face for emoji codepoints, falling back
// to the style face when the glyph is missing.
func faceForRune(r rune, size float64, st style) font.Face {
	if isEmoji(r) {
		if f, err := emojiFaceFor(size); err == nil {
			if _, ok := f.GlyphAdvance(r); ok {
				return f
			}
		}
	}
	f, _ := faceFor(size, st)
	return f
}

// runeWidth measures a single rune with its resolved face.
func runeWidth(r rune, size float64, st style) int {
	f := faceForRune(r, size, st)
	adv, ok := f.GlyphAdvance(r)
	if !ok {
		return 0
	}
	return adv.Ceil()
}

// wordWidth measures a word; space reports the width of a normal
// space in the word's style (for inter-word gaps).
func wordWidth(w word, size float64) (total, space int) {
	f, _ := faceFor(size, w.st)
	sp, _ := f.GlyphAdvance(' ')
	for _, r := range w.text {
		total += runeWidth(r, size, w.st)
	}
	return total, sp.Ceil()
}

// wrap breaks a block's words into lines of at most maxW pixels,
// hard-breaking overlong words rune-wise.
func wrap(words []word, size float64, maxW int) [][]word {
	var lines [][]word
	var cur []word
	curW := 0

	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
		cur, curW = nil, 0
	}

	for _, w := range words {
		if w.br {
			flush()
			continue
		}
		ww, sp := wordWidth(w, size)
		gap := 0
		if w.spaceBefore && len(cur) > 0 {
			gap = sp
		}
		if curW+gap+ww <= maxW {
			cur = append(cur, w)
			curW += gap + ww
			continue
		}
		flush()
		// Hard-break a word wider than the line, rune-wise.
		if ww > maxW {
			line := 0
			var run []rune
			for _, r := range w.text {
				rw := runeWidth(r, size, w.st)
				if line+rw > maxW && len(run) > 0 {
					lines = append(lines, []word{{text: string(run), st: w.st}})
					run, line = nil, 0
				}
				run = append(run, r)
				line += rw
			}
			if len(run) > 0 {
				cur = []word{{text: string(run), st: w.st}}
				curW = line
			}
			continue
		}
		cur = append(cur, w)
		curW = ww
	}
	flush()
	return lines
}

// render lays out all blocks and paints the canvas.
func (b *builder) render() (*image.RGBA, error) {
	maxW := PaperWidth - 2*PageMargin

	type laidLine struct {
		words []word
		width int
	}
	type laidBlock struct {
		blk      block
		lines    []laidLine
		lineH    int
		ascent   int
		markerAscent int
	}

	// Pass 1: wrap and measure to compute total height.
	var laid []laidBlock
	height := 0
	for _, blk := range b.blocks {
		lb := laidBlock{blk: blk}
		if blk.rule {
			lb.lineH = 2
			height += blk.spaceBefore + 2 + blk.spaceAfter
			laid = append(laid, lb)
			continue
		}
		lb.lineH = int(blk.size*lineHeight) + 1
		f, _ := faceFor(blk.size, style{})
		m := f.Metrics()
		lb.ascent = m.Ascent.Ceil()
		lb.markerAscent = lb.ascent
		for _, line := range wrap(blk.words, blk.size, maxW-len(blk.marker)*runeWidth('0', blk.size, style{})/2) {
			w := 0
			for i, word := range line {
				ww, sp := wordWidth(word, blk.size)
				if i > 0 && word.spaceBefore {
					w += sp
				}
				w += ww
			}
			lb.lines = append(lb.lines, laidLine{words: line, width: w})
		}
		height += blk.spaceBefore + len(lb.lines)*lb.lineH + blk.spaceAfter
		if height > maxHeight {
			return nil, fmt.Errorf("document too tall")
		}
		laid = append(laid, lb)
	}
	if height > maxHeight {
		return nil, fmt.Errorf("document too tall")
	}
	if height == 0 {
		height = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, PaperWidth, height))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)

	// Pass 2: paint.
	d := &font.Drawer{Src: image.Black, Dst: dst}
	y := 0
	for _, lb := range laid {
		y += lb.blk.spaceBefore
		if lb.blk.rule {
			fillRect(dst, image.Rect(PageMargin, y, PaperWidth-PageMargin, y+2), image.Black)
			y += 2 + lb.blk.spaceAfter
			continue
		}
		indent := 0
		if lb.blk.marker != "" {
			indent = len(lb.blk.marker) * runeWidth('0', lb.blk.size, style{}) / 2
		}
		for _, line := range lb.lines {
			x := PageMargin + indent
			switch lb.blk.align {
			case 1:
				x = PageMargin + (maxW-line.width)/2
			case 2:
				x = PageMargin + maxW - line.width
			}
			for i, w := range line.words {
				if i > 0 && w.spaceBefore {
					_, sp := wordWidth(w, lb.blk.size)
					x += sp
				}
				baseline := y + lb.ascent
				for _, r := range w.text {
					f := faceForRune(r, lb.blk.size, w.st)
					d.Face = f
					d.Dot = fixed.P(x, baseline)
					d.DrawString(string(r))
					adv, ok := f.GlyphAdvance(r)
					if !ok {
						continue
					}
					x += adv.Ceil()
				}
			}
			y += lb.lineH
		}
		// List markers, drawn once at the block's first baseline.
		if lb.blk.marker != "" {
			f, _ := faceFor(lb.blk.size, style{})
			d.Face = f
			d.Dot = fixed.P(PageMargin, y - lb.blk.spaceAfter - lb.lineH*len(lb.lines) + lb.markerAscent)
			d.DrawString(lb.blk.marker)
		}
		y += lb.blk.spaceAfter
	}
	return dst, nil
}

// fillRect paints a rectangle black.
func fillRect(dst *image.RGBA, r image.Rectangle, c *image.Uniform) {
	draw.Draw(dst, r, c, image.Point{}, draw.Src)
}

// toPixels converts the drawn canvas to 1-bit pixels via the shared
// Atkinson pipeline in pixels; text is already bilevel, so only
// antialiased edges (emoji outlines, hinting) are affected.
func toPixels(img *image.RGBA) ([]bool, int, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, err
	}
	return pixels.FromImage(buf.Bytes())
}
