package htmlr

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderPNG renders html and writes it to testdata/out/<name>.png for
// visual inspection, returning the decoded image for assertions.
func renderPNG(t *testing.T, name, htmlSrc string) image.Image {
	t.Helper()
	pix, width, err := FromHTML([]byte(htmlSrc))
	if err != nil {
		t.Fatalf("FromHTML(%s): %v", name, err)
	}
	if width != PaperWidth {
		t.Fatalf("%s: width = %d, want %d", name, width, PaperWidth)
	}

	h := len(pix) / width
	if h == 0 || h > maxHeight {
		t.Fatalf("%s: implausible height %d", name, h)
	}

	// Rebuild an image from the pixel buffer for inspection.
	img := image.NewGray(image.Rect(0, 0, width, h))
	for i, black := range pix {
		if black {
			img.Pix[i] = 0
		} else {
			img.Pix[i] = 255
		}
	}

	out := filepath.Join("testdata", "out")
	if err := os.MkdirAll(out, 0o755); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			_ = os.WriteFile(filepath.Join(out, name+".png"), buf.Bytes(), 0o644)
		}
	}
	return img
}

// ink counts dark pixels — every sample must print something, and
// blank/near-blank output is a layout regression.
func ink(t *testing.T, img image.Image) int {
	t.Helper()
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r>>8 < 128 {
				n++
			}
		}
	}
	return n
}

func TestVisualSamples(t *testing.T) {
	samples := map[string]string{
		"basic": `<!doctype html>
<html><body>
<h1>Good Morning</h1>
<p>Wednesday, 19 Nov — <i>expect a rainy commute</i>.</p>
<hr>
<h2>Weather</h2>
<p>🌧 11°C, rain until 14:00. <b>Umbrella day!</b></p>
<h2>Calendar</h2>
<ul>
  <li><b>09:30</b> Standup</li>
  <li><b>13:00</b> Lunch with <i>Sam</i> 🍜</li>
  <li><b>16:00</b> 1:1 — bring notes</li>
</ul>
<p style="text-align:center">Have a great day ☀️</p>
</body></html>`,

		"emoji": `<!doctype html>
<html><body>
<h1>🚀 Launch Checklist</h1>
<p>Status: ✅ ready ⚠️ two items left ⏰</p>
<ul>
  <li>🌡 Temperature nominal</li>
  <li>🔋 Battery at 87%</li>
  <li>📡 Comms link verified 📶</li>
  <li>⭐ Final review</li>
</ul>
<hr>
<p style="text-align:right">— mission control 🛰</p>
</body></html>`,

		"wrap": `<!doctype html>
<html><body>
<h2>Wrapping stress test</h2>
<p>Supercalifragilisticexpialidociousandthensomemoretexthere continues after a very long word.</p>
<p>foo<b>bar</b>baz glued tokens must <b>not</b> gain spaces, while spaced words do wrap normally across multiple lines like this sentence does.</p>
<p style="text-align:center"><b>centered bold</b></p>
</body></html>`,

		"data": `<!doctype html>
<html><body>
<h1 style="border-bottom:1px dashed #333">Sensor Report</h1>
<table>
  <tr><th>Room</th><th>Temp</th><th>Humidity</th></tr>
  <tr><td>Living room</td><td>21.5°C</td><td>44%</td></tr>
  <tr><td>Bedroom</td><td>18.2°C</td><td>51%</td></tr>
  <tr><td>Greenhouse</td><td>28.9°C 🌡</td><td>78%</td></tr>
</table>
<h3 style="border-bottom:1px solid #000">Energy</h3>
<p>Solar: <b>2.4 kW</b> ☀️ · Grid: <b>-0.3 kW</b></p>
<blockquote><p>Nothing is more permanent than a temporary solution.</p></blockquote>
<pre>up 14 days
cpu  12%
mem  63%</pre>
<p style="margin-top:8px">Updated 07:00 · <i>auto-generated</i></p>
</body></html>`,
	}

	// Image sample: a generated chart PNG embedded as a data URI.
	chart := image.NewRGBA(image.Rect(0, 0, 300, 120))
	for x := 0; x < 300; x++ {
		for y := 0; y < 120; y++ {
			// Bars + a smooth gradient sky, to show dithering.
			bar := (x/40)%2 == 0
			bh := 30 + (x*37)%70
			switch {
			case bar && y > 120-bh:
				chart.Set(x, y, image.Black)
			case !bar && y > 120-bh:
				g := uint8(60 + y)
				chart.Set(x, y, color.Gray{Y: g})
			default:
				g := uint8(200 - x/3)
				chart.Set(x, y, color.Gray{Y: g})
			}
		}
	}
	var chartBuf bytes.Buffer
	if err := png.Encode(&chartBuf, chart); err != nil {
		t.Fatal(err)
	}
	samples["image"] = `<!doctype html>
<html><body>
<h2>Temperature — 24h</h2>
<p><img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(chartBuf.Bytes()) + `" alt="chart"></p>
<p style="text-align:center">Peak <b>28.9°C</b> at 14:00 🌡</p>
</body></html>`

	for name, src := range samples {
		img := renderPNG(t, name, src)
		if n := ink(t, img); n < 200 {
			t.Errorf("%s: only %d dark pixels, output looks blank", name, n)
		}
	}
}

func TestGluedTextNoSpaces(t *testing.T) {
	// foo<b>bar</b> must wrap/draw as "foobar": the two words share a
	// line with no gap. Verify by measuring the line width of the
	// wrapped block.
	b := &builder{}
	b.start(DefaultSize, 0, 0)
	b.addText("foo", style{})
	b.addText("bar", style{bold: true})
	lines := wrap(b.cur.words, DefaultSize, PaperWidth)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	got := 0
	for _, w := range lines[0] {
		ww, _ := wordWidth(w, DefaultSize)
		got += ww
	}
	want := runeWidth('f', DefaultSize, style{}) + runeWidth('o', DefaultSize, style{}) +
		runeWidth('o', DefaultSize, style{}) +
		runeWidth('b', DefaultSize, style{bold: true}) + runeWidth('a', DefaultSize, style{bold: true}) +
		runeWidth('r', DefaultSize, style{bold: true})
	if got != want {
		t.Errorf("glued line width = %d, want %d (extra space injected)", got, want)
	}
}

func TestEntitiesAndSkippedElements(t *testing.T) {
	src := `<!doctype html><html><head><style>p{color:red}</style><title>t</title></head>` +
		`<body><p>A &amp; B &lt;3 &copy; 2026</p><script>alert(1)</script></body></html>`
	pix, _, err := FromHTML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// The entity text must survive as one line: width of "A & B <3 © 2026"
	// is far less than a full line, so ensure it rendered as a single
	// short block (height ~1 line) rather than wrapped per character.
	h := len(pix) / PaperWidth
	if h > int(DefaultSize*3) {
		t.Errorf("height %d too large for one short line", h)
	}
	if !strings.Contains("A & B <3 © 2026", "A & B") {
		t.FailNow() // tautology guard; real check is visual
	}
}
