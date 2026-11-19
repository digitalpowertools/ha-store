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
	align       int    // 0 left, 1 center, 2 right
	marker      string // list bullet / number, drawn in the left gutter
	rule        bool   // <hr>
	border      string // "", "solid", "dashed" — border-bottom under the block
	indent      int    // extra left inset in px
	quote       bool   // draw a blockquote bar in the gutter
	table       *tableDef
	spaceBefore int
	spaceAfter  int
}

// tableDef holds a simple grid: rows of cells, no colspan/rowspan.
// Column widths are computed from content and scaled to paper width.
type tableDef struct {
	rows []tableRow
}

type tableRow struct {
	cells []cell
}

type cell struct {
	words  []word
	header bool
}

const (
	cellPadX   = 6
	tableLineH = 1.25
	minColW    = 40
	// minTableSize is the smallest font a table will shrink to before
	// columns are squeezed proportionally.
	minTableSize = 16
)

type builder struct {
	blocks   []block
	cur      *block
	pending  bool     // whitespace pending before the next word
	listKind []string // stack of open list kinds ("ul"/"ol")
	listN    []int    // ol counters parallel to listKind
	indent   int      // current extra indent (blockquotes)
	quotes   int      // blockquote nesting depth
	inTable  bool
}

func (b *builder) flush() {
	if b.cur != nil && (len(b.cur.words) > 0 || b.cur.rule || b.cur.table != nil) {
		b.blocks = append(b.blocks, *b.cur)
	}
	b.cur = nil
	b.pending = false
}

// start opens a new block with the given base style and spacing,
// inheriting the current indent and quote context.
func (b *builder) start(size float64, before, after int) {
	b.flush()
	b.cur = &block{size: size, spaceBefore: before, spaceAfter: after,
		indent: b.indent, quote: b.quotes > 0}
}

// addText appends inline text in the given style, collapsing
// whitespace and recording word boundaries.
func (b *builder) addText(s string, st style) {
	if b.cur == nil {
		b.cur = &block{size: DefaultSize, indent: b.indent, quote: b.quotes > 0}
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
		case "pre":
			b.flush()
			b.cur = &block{
				size:        DefaultSize * 0.85,
				spaceBefore: 10, spaceAfter: 10,
				indent: b.indent, quote: b.quotes > 0,
			}
			// Preserve whitespace: spaces become NBSP so word-splitting
			// keeps indentation intact; long lines hard-break rune-wise.
			for _, line := range strings.Split(rawText(n), "\n") {
				if strings.TrimSpace(line) != "" {
					b.addText(strings.ReplaceAll(line, " ", "\u00a0"), style{mono: true})
				}
				b.cur.words = append(b.cur.words, word{br: true})
			}
			b.flush()
			return
		case "blockquote":
			b.flush()
			b.quotes++
			b.indent += 18
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				b.walk(c, st)
			}
			b.indent -= 18
			b.quotes--
			return
		case "table":
			b.flush()
			t := &tableDef{}
			b.cur = &block{size: DefaultSize, spaceBefore: 10, spaceAfter: 14, table: t}
			b.inTable = true
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				b.walk(c, st)
			}
			b.inTable = false
			b.flush()
			return
		case "tr":
			if b.inTable && b.cur != nil && b.cur.table != nil {
				b.cur.table.rows = append(b.cur.table.rows, tableRow{})
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					b.walk(c, st)
				}
				return
			}
		case "th", "td":
			if b.inTable && b.cur != nil && b.cur.table != nil && len(b.cur.table.rows) > 0 {
				row := &b.cur.table.rows[len(b.cur.table.rows)-1]
				row.cells = append(row.cells, cell{
					words:  b.inlineWords(n, st, n.Data == "th"),
					header: n.Data == "th",
				})
				return
			}
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

// rawText concatenates all text nodes under n, preserving newlines.
func rawText(n *html.Node) string {
	var sb strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// inlineWords collects the inline content of n into a word list,
// used for table cells. Block elements inside a cell are flattened.
func (b *builder) inlineWords(n *html.Node, st style, header bool) []word {
	var words []word
	pending := false
	var emit func(n *html.Node, st style)
	emit = func(n *html.Node, st style) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "br":
				pending = true // treat as a space in cells
				return
			case "b", "strong":
				st.bold = true
			case "i", "em":
				st.italic = true
			case "code", "tt":
				st.mono = true
			}
		}
		if n.Type == html.TextNode {
			s := strings.Map(func(r rune) rune {
				if r == '\n' || r == '\t' || r == '\r' {
					return ' '
				}
				return r
			}, n.Data)
			for _, tok := range strings.Split(s, " ") {
				if tok == "" {
					pending = true
					continue
				}
				words = append(words, word{text: tok, st: st, spaceBefore: pending})
				pending = true
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			emit(c, st)
		}
	}
	if header {
		st.bold = true
	}
	emit(n, st)
	return words
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
		case "margin":
			if n, err := strconv.Atoi(strings.TrimSuffix(v, "px")); err == nil && n >= 0 {
				blk.spaceBefore, blk.spaceAfter = n, n
			}
		case "margin-top":
			if n, err := strconv.Atoi(strings.TrimSuffix(v, "px")); err == nil && n >= 0 {
				blk.spaceBefore = n
			}
		case "margin-bottom":
			if n, err := strconv.Atoi(strings.TrimSuffix(v, "px")); err == nil && n >= 0 {
				blk.spaceAfter = n
			}
		case "margin-left", "padding-left":
			if n, err := strconv.Atoi(strings.TrimSuffix(v, "px")); err == nil && n >= 0 {
				blk.indent += n
			}
		case "border-bottom":
			// e.g. "1px dashed #333" — color is ignored (1-bit paper).
			switch {
			case strings.Contains(v, "dashed"), strings.Contains(v, "dotted"):
				blk.border = "dashed"
			case strings.Contains(v, "solid"):
				blk.border = "solid"
			}
		case "border-bottom-style":
			switch v {
			case "dashed", "dotted":
				blk.border = "dashed"
			case "solid":
				blk.border = "solid"
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

// layoutTable computes column widths from min-content (the widest
// single word per column, so multi-word cells wrap instead of
// demanding full-line width). When the total exceeds the paper, the
// font is shrunk (down to minTableSize) before columns are squeezed.
func layoutTable(t *tableDef, size float64, maxW int) (cols []int, finalSize float64) {
	finalSize = size
	n := 0
	for _, r := range t.rows {
		n = max(n, len(r.cells))
	}
	if n == 0 {
		return nil, finalSize
	}

	minContent := func(sz float64) []int {
		mc := make([]int, n)
		for _, r := range t.rows {
			for i, c := range r.cells {
				if i >= n {
					break
				}
				for _, w := range c.words {
					if w.br {
						continue
					}
					mc[i] = max(mc[i], wWidth(w, sz))
				}
			}
		}
		return mc
	}

	pads := n * 2 * cellPadX
	avail := maxW - pads
	mc := minContent(size)
	total := 0
	for _, w := range mc {
		total += w
	}

	if total > avail {
		// Try shrinking the font: content scales ~linearly with size.
		if shrunk := size * float64(avail) / float64(total); shrunk >= minTableSize {
			finalSize = shrunk
			mc = minContent(finalSize)
			total = 0
			for _, w := range mc {
				total += w
			}
		}
	}

	if total > avail && total > 0 {
		for i := range mc {
			mc[i] = max(minColW-2*cellPadX, mc[i]*avail/total)
		}
	}
	// Each column carries its own horizontal padding.
	cols = make([]int, n)
	for i, w := range mc {
		cols[i] = w + 2*cellPadX
	}
	return cols, finalSize
}

// drawRule paints a horizontal 1px rule; dashed uses a 5-on/4-off
// pattern for the classic BERG look.
func drawRule(dst *image.RGBA, x0, x1, y int, dashed bool) {
	if dashed {
		for x := x0; x < x1; x += 9 {
			fillRect(dst, image.Rect(x, y, min(x+5, x1), y+1), image.Black)
		}
		return
	}
	fillRect(dst, image.Rect(x0, y, x1, y+1), image.Black)
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
		borderGap int
		// table state
		cols     []int
		rowLines [][][]laidLine // per row, per cell
	}
	const borderGap = 5

	// Pass 1: wrap and measure to compute total height.
	var laid []laidBlock
	height := 0
	for _, blk := range b.blocks {
		lb := laidBlock{blk: blk}
		if blk.rule {
			height += blk.spaceBefore + 2 + blk.spaceAfter
			laid = append(laid, lb)
			continue
		}
		if blk.table != nil {
			cols, size2 := layoutTable(blk.table, blk.size, maxW-blk.indent)
			lb.blk.size = size2
			blk.size = size2
			lb.cols = cols
			lb.lineH = int(blk.size*tableLineH) + 1
			f, _ := faceFor(blk.size, style{})
			lb.ascent = f.Metrics().Ascent.Ceil()
			for _, row := range blk.table.rows {
				lines := make([][]laidLine, len(row.cells))
				maxLines := 1
				for ci, c := range row.cells {
					if ci >= len(lb.cols) {
						break
					}
					for _, w := range wrap(c.words, blk.size, lb.cols[ci]-2*cellPadX) {
						wd := 0
						for i, word := range w {
							ww, sp := wordWidth(word, blk.size)
							if i > 0 && word.spaceBefore {
								wd += sp
							}
							wd += ww
						}
						lines[ci] = append(lines[ci], laidLine{words: w, width: wd})
					}
					maxLines = max(maxLines, len(lines[ci]))
				}
				lb.rowLines = append(lb.rowLines, lines)
				// 2px vertical padding above and below the row.
				height += 2*2 + maxLines*lb.lineH
				isHeader := len(row.cells) > 0 && row.cells[0].header
				if isHeader {
					height += 3
				}
			}
			height += blk.spaceBefore + blk.spaceAfter
			if height > maxHeight {
				return nil, fmt.Errorf("document too tall")
			}
			laid = append(laid, lb)
			continue
		}

		lb.lineH = int(blk.size*lineHeight) + 1
		f, _ := faceFor(blk.size, style{})
		m := f.Metrics()
		lb.ascent = m.Ascent.Ceil()
		markerW := len(blk.marker) * runeWidth('0', blk.size, style{}) / 2
		if blk.border != "" {
			lb.borderGap = borderGap
		}
		for _, line := range wrap(blk.words, blk.size, maxW-blk.indent-markerW) {
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
		height += blk.spaceBefore + len(lb.lines)*lb.lineH + lb.borderGap + blk.spaceAfter
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
		blk := lb.blk
		y += blk.spaceBefore
		if blk.rule {
			fillRect(dst, image.Rect(PageMargin, y, PaperWidth-PageMargin, y+2), image.Black)
			y += 2 + blk.spaceAfter
			continue
		}

		// Quote bar: drawn per block, from first to last baseline.
		quoteTop, quoteBottom := -1, -1

		if blk.table != nil {
			for ri, row := range blk.table.rows {
				if ri >= len(lb.rowLines) {
					break
				}
				cells := lb.rowLines[ri]
				maxLines := 1
				for _, lines := range cells {
					maxLines = max(maxLines, len(lines))
				}
				x := PageMargin + blk.indent
				for ci, lines := range cells {
					if ci >= len(lb.cols) {
						break
					}
					cx := x + cellPadX
					for li, line := range lines {
						baseline := y + 2 + li*lb.lineH + lb.ascent
						px := cx
						for i, w := range line.words {
							if i > 0 && w.spaceBefore {
								_, sp := wordWidth(w, blk.size)
								px += sp
							}
							for _, r := range w.text {
								f := faceForRune(r, blk.size, w.st)
								adv, ok := f.GlyphAdvance(r)
								if !ok {
									continue
								}
								d.Face = f
								d.Dot = fixed.P(px, baseline)
								d.DrawString(string(r))
								px += adv.Ceil()
							}
						}
					}
					x += lb.cols[ci]
				}
				y += 2 + maxLines*lb.lineH + 2
				if len(row.cells) > 0 && row.cells[0].header {
					drawRule(dst, PageMargin+blk.indent, PaperWidth-PageMargin, y, false)
					y += 3
				}
			}
			y += blk.spaceAfter
			continue
		}

		markerW := 0
		if blk.marker != "" {
			markerW = len(blk.marker) * runeWidth('0', blk.size, style{}) / 2
		}
		for _, line := range lb.lines {
			x := PageMargin + blk.indent + markerW
			switch blk.align {
			case 1:
				x = PageMargin + blk.indent + (maxW-blk.indent-line.width)/2
			case 2:
				x = PageMargin + maxW - line.width
			}
			if quoteTop < 0 {
				quoteTop = y
			}
			for i, w := range line.words {
				if i > 0 && w.spaceBefore {
					_, sp := wordWidth(w, blk.size)
					x += sp
				}
				baseline := y + lb.ascent
				for _, r := range w.text {
					f := faceForRune(r, blk.size, w.st)
					adv, ok := f.GlyphAdvance(r)
					if !ok {
						continue // rune has no glyph (e.g. stray FE0F)
					}
					d.Face = f
					d.Dot = fixed.P(x, baseline)
					d.DrawString(string(r))
					x += adv.Ceil()
				}
			}
			y += lb.lineH
		}
		quoteBottom = y

		// List markers, drawn once at the block's first baseline.
		if blk.marker != "" {
			f, _ := faceFor(blk.size, style{})
			d.Face = f
			d.Dot = fixed.P(PageMargin+blk.indent, y - blk.spaceAfter - lb.borderGap - lb.lineH*len(lb.lines) + lb.ascent)
			d.DrawString(blk.marker)
		}

		if blk.border != "" {
			y += borderGap - 2
			drawRule(dst, PageMargin+blk.indent, PaperWidth-PageMargin, y, blk.border == "dashed")
			y += 2
		}

		if blk.quote && quoteTop >= 0 {
			fillRect(dst, image.Rect(PageMargin, quoteTop, PageMargin+2, quoteBottom), image.Black)
		}
		y += blk.spaceAfter
	}
	return dst, nil
}

// wWidth measures a word without its leading space.
func wWidth(w word, size float64) int {
	total := 0
	for _, r := range w.text {
		total += runeWidth(r, size, w.st)
	}
	return total
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
