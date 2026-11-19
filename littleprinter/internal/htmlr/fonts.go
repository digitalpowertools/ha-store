package htmlr

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// Embedded Noto Emoji (monochrome), © Google, SIL Open Font License.
// Version history: https://github.com/googlefonts/noto-emoji
//
//go:embed notoemoji.ttf
var notoEmojiTTF []byte

var (
	facesOnce sync.Once
	facesErr  error
	// Parsed source fonts.
	regFont, boldFont, italFont, monoFont, emojiFont *opentype.Font
)

func loadFonts() {
	facesOnce.Do(func() {
		for _, e := range []struct {
			data []byte
			dst  **opentype.Font
		}{
			{goregular.TTF, &regFont},
			{gobold.TTF, &boldFont},
			{goitalic.TTF, &italFont},
			{gomono.TTF, &monoFont},
			{notoEmojiTTF, &emojiFont},
		} {
			f, err := opentype.Parse(e.data)
			if err != nil {
				facesErr = err
				return
			}
			*e.dst = f
		}
	})
}

// faceKey identifies a cached font.Face. Emoji glyphs always use the
// emoji face, so it is not part of the key.
type faceKey struct {
	size          int
	bold, italic, mono bool
}

var (
	faceCacheMu sync.Mutex
	faceCache   = map[faceKey]font.Face{}
	emojiFaces  = map[int]font.Face{}
)

// faceFor returns the face for a style at the given pixel size.
func faceFor(size float64, st style) (font.Face, error) {
	loadFonts()
	if facesErr != nil {
		return nil, facesErr
	}

	k := faceKey{size: int(size), bold: st.bold, italic: st.italic, mono: st.mono}
	faceCacheMu.Lock()
	defer faceCacheMu.Unlock()

	if f, ok := faceCache[k]; ok {
		return f, nil
	}

	src := regFont
	switch {
	case st.mono:
		src = monoFont
	case st.bold && st.italic:
		// No bold-italic in the embedded set; bold wins.
		src = boldFont
	case st.bold:
		src = boldFont
	case st.italic:
		src = italFont
	}

	f, err := opentype.NewFace(src, &opentype.FaceOptions{
		Size:    float64(k.size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	faceCache[k] = f
	return f, nil
}

// emojiFaceFor returns the monochrome emoji face scaled to size.
func emojiFaceFor(size float64) (font.Face, error) {
	loadFonts()
	if facesErr != nil {
		return nil, facesErr
	}
	faceCacheMu.Lock()
	defer faceCacheMu.Unlock()
	k := int(size)
	if f, ok := emojiFaces[k]; ok {
		return f, nil
	}
	f, err := opentype.NewFace(emojiFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	emojiFaces[k] = f
	return f, nil
}
