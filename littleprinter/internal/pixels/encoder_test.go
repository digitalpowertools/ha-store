package pixels

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// Golden fixtures from sirius/coding/test_encoders.py. The 2x2 image is
// a checkerboard: (0,0) white, (1,0) black, (0,1) black, (1,1) white.
var checkerboard2x2 = []bool{
	false, true,
	true, false,
}

// Expected binary_payloads, copied verbatim from test_encoders.py.
var goldenPayloads = map[uint16]string{
	CmdSetDeliveryAndPrint:       "AQABAAEAAAAAAAAAJwAAACMAAAAAABUAAAAdcwPoHWHQHS8PHUSAGyoAAAAAADABAwAAAAECAQ==",
	CmdSetDelivery:               "AQACAAEAAAAAAAAAJwAAACMAAAAAABUAAAAdcwPoHWHQHS8PHUSAGyoAAAAAADABAwAAAAECAQ==",
	CmdSetDeliveryAndPrintNoFace: "AQARAAEAAAAAAAAAJwAAACMAAAAAABUAAAAdcwPoHWHQHS8PHUSAGyoAAAAAADABAwAAAAECAQ==",
	CmdSetDeliveryNoFace:         "AQASAAEAAAAAAAAAJwAAACMAAAAAABUAAAAdcwPoHWHQHS8PHUSAGyoAAAAAADABAwAAAAECAQ==",
}

func TestEncodePrintGolden(t *testing.T) {
	for cmd, want := range goldenPayloads {
		got := EncodePrint(cmd, checkerboard2x2, 2, 1)
		gotB64 := base64.StdEncoding.EncodeToString(got)
		if gotB64 != want {
			t.Errorf("command %#x:\n got  %s\n want %s", cmd, gotB64, want)
		}
	}
}

func TestRLEFromBWCheckerboard(t *testing.T) {
	// Rotation-180 of the checkerboard is itself; runs: W1 B2 W1.
	count, encoded := RLEFromBW(checkerboard2x2, 2)
	if count != 4 {
		t.Fatalf("pixel count = %d, want 4", count)
	}
	want := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x02, 0x01}
	if !bytes.Equal(encoded, want) {
		t.Errorf("RLE blob:\n got  %x\n want %x", encoded, want)
	}
}

func TestRLERunSplitting(t *testing.T) {
	// Port of image_encoding.rle chunking semantics.
	cases := []struct {
		lengths []int
		want    []byte
	}{
		{[]int{0}, []byte{0}},
		{[]int{1, 2, 1}, []byte{1, 2, 1}},
		{[]int{252}, []byte{251, 0, 1}},
		{[]int{300}, []byte{251, 0, 49}},
		{[]int{400}, []byte{252, 0, 16}},
		{[]int{1600}, []byte{255, 0, 64}},
		{[]int{4000}, []byte{255, 0, 255, 0, 253, 0, 160}},
		// A black-first image gets a zero-length white run prepended.
	}
	for i, c := range cases {
		got := rleRuns(c.lengths)
		if !bytes.Equal(got, c.want) {
			t.Errorf("case %d lengths %v:\n got  %v\n want %v", i, c.lengths, got, c.want)
		}
	}
}

func TestRLEBlackFirst(t *testing.T) {
	// Single black pixel: decoder assumes first run is white, so the
	// encoder prepends a zero-length white run.
	count, encoded := RLEFromBW([]bool{true}, 1)
	if count != 1 {
		t.Fatalf("pixel count = %d, want 1", count)
	}
	// header: 01 | len=2 | 0x00 (white, 0) | 0x01 (black, 1)
	want := []byte{0x01, 0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	if !bytes.Equal(encoded, want) {
		t.Errorf("RLE blob:\n got  %x\n want %x", encoded, want)
	}
}

func TestTextRenderProducesPixels(t *testing.T) {
	pix, width, err := FromText("Hello, Little Printer!")
	if err != nil {
		t.Fatal(err)
	}
	if width != PaperWidth {
		t.Fatalf("width = %d, want %d", width, PaperWidth)
	}
	black := 0
	for _, p := range pix {
		if p {
			black++
		}
	}
	if black == 0 {
		t.Fatal("no black pixels rendered")
	}
	if len(pix)%width != 0 {
		t.Fatalf("pixel count %d not a multiple of width %d", len(pix), width)
	}
}
