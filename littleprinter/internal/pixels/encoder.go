// Package pixels ports sirius/coding/image_encoding.py (the RLE encoder)
// and sirius/coding/encoders.py (the binary payload builders).
package pixels

import (
	"encoding/binary"
)

const (
	// White/black as booleans: true means black (toner on paper).
	threshold = 127
)

// translate mirrors image_encoding.TRANSLATE: chunks of runs and their
// one-byte RLE codes.
var translate = []struct {
	chunk int
	code  byte
}{
	{1536, 255},
	{1152, 254},
	{768, 253},
	{384, 252},
	{251, 251},
}

// rleRuns ports image_encoding.rle: takes run lengths (alternating
// white/black, starting with white) and yields the RLE byte stream.
func rleRuns(lengths []int) []byte {
	out := make([]byte, 0, len(lengths)+16)
	emit := func(b byte) { out = append(out, b) }

	for _, length := range lengths {
		if length == 0 {
			emit(0)
			continue
		}
		if length <= 251 {
			emit(byte(length))
			continue
		}
		remainder := length
		doneFirst := false
		for remainder > 251 {
			var chunk int
			var code byte
			for _, t := range translate {
				if remainder >= t.chunk {
					chunk, code = t.chunk, t.code
					break
				}
			}
			remainder -= chunk
			if doneFirst {
				emit(0)
			} else {
				doneFirst = true
			}
			emit(code)
		}
		// Python `while...else`: runs when the loop exits normally.
		if remainder > 0 {
			emit(0)
			emit(byte(remainder))
		}
	}
	return out
}

// RLEFromBW ports image_encoding.rle_from_bw. Input is the image's
// pixels in row-major order, width pixels wide, true = black. The image
// is rotated 180 degrees first (the printer expects the stream bottom-up).
//
// Returns (pixelCount, encoded) where encoded is:
//
//	0x01 | uint32 LE len(rle) | rle bytes
func RLEFromBW(pixels []bool, width int) (pixelCount int, encoded []byte) {
	if width <= 0 || len(pixels)%width != 0 {
		panic("pixels: bad width")
	}
	h := len(pixels) / width

	// Rotate 180: reverse the pixel sequence.
	rotated := make([]bool, len(pixels))
	for i, p := range pixels {
		rotated[len(pixels)-1-i] = p
	}

	// Group into runs. Decoder assumes the first run is white; if the
	// first pixel is black, insert a zero-length white run.
	type run struct {
		black bool
		len   int
	}
	var runs []run
	for _, p := range rotated {
		if len(runs) > 0 && runs[len(runs)-1].black == p {
			runs[len(runs)-1].len++
		} else {
			runs = append(runs, run{p, 1})
		}
	}
	if len(runs) > 0 && runs[0].black {
		runs = append([]run{{false, 0}}, runs...)
	}

	lengths := make([]int, len(runs))
	for i, r := range runs {
		lengths[i] = r.len
	}
	rle := rleRuns(lengths)

	pixelCount = len(pixels)

	// package: struct.pack("<BL", 0x01, len(rle)) + rle
	encoded = make([]byte, 5+len(rle))
	encoded[0] = 0x01
	binary.LittleEndian.PutUint32(encoded[1:5], uint32(len(rle)))
	copy(encoded[5:], rle)
	_ = h
	return pixelCount, encoded
}

// encodePixels ports encoders._encode_pixels: wraps RLE data with the
// printer control block.
func encodePixels(pixelCount int, rle []byte) []byte {
	printerControl := []byte{
		0x1d, 0x73, 0x03, 0xe8, // max printer speed
		0x1d, 0x61, 0xd0, // printer acceleration
		0x1d, 0x2f, 0x0f, // peak current
		0x1d, 0x44, 0x80, // max intensity
	}
	printerByteCount := pixelCount / 8
	n3 := printerByteCount / 65536
	rem := printerByteCount % 65536
	n2 := rem / 256
	n1 := rem % 256
	printerData := []byte{0x1b, 0x2a, byte(n1), byte(n2), byte(n3), 0, 0, 48}

	headerRegion := make([]byte, 0, 5+len(printerControl)+len(printerData))
	headerRegion = append(headerRegion, 0) // struct "<B"
	var l [4]byte
	binary.LittleEndian.PutUint32(l[:], uint32(len(printerControl)+len(printerData)))
	headerRegion = append(headerRegion, l[:]...)
	headerRegion = append(headerRegion, printerControl...)
	headerRegion = append(headerRegion, printerData...)

	// payload: struct.pack("<IB", len(header_region)+len(rle)+1, 0)
	payload := make([]byte, 0, 5+len(headerRegion)+len(rle))
	binary.LittleEndian.PutUint32(l[:], uint32(len(headerRegion)+len(rle)+1))
	payload = append(payload, l[:]...)
	payload = append(payload, 0)
	payload = append(payload, headerRegion...)
	payload = append(payload, rle...)
	return payload
}

// Command IDs from encoders.encode_bridge_command.
const (
	CmdSetDeliveryAndPrint       uint16 = 0x1
	CmdSetDelivery               uint16 = 0x2
	CmdSetDeliveryAndPrintNoFace uint16 = 0x11
	CmdSetDeliveryNoFace         uint16 = 0x12
	CmdSetPersonality            uint16 = 0x0102
	CmdSetPersonalityWithMessage uint16 = 0x0101
	CmdSetQuip                   uint16 = 0x0202
)

// encodePrinterMessage ports encoders._encode_printer_message.
func encodePrinterMessage(command uint16, payload []byte, printID uint32) []byte {
	const littlePrinterDeviceID = 1

	// device type c, reserved c, command <H, file_id <I, crc <I (zero)
	out := make([]byte, 0, 12+4+len(payload))
	out = append(out, littlePrinterDeviceID, 0)
	var b [4]byte
	binary.LittleEndian.PutUint16(b[:2], command)
	out = append(out, b[0], b[1])
	binary.LittleEndian.PutUint32(b[:], printID)
	out = append(out, b[0], b[1], b[2], b[3])
	binary.LittleEndian.PutUint32(b[:], 0) // unimplemented CRC
	out = append(out, b[0], b[1], b[2], b[3])

	binary.LittleEndian.PutUint32(b[:], uint32(len(payload)))
	out = append(out, b[0], b[1], b[2], b[3])
	out = append(out, payload...)
	return out
}

// EncodePrint builds the binary_payload for a SetDeliveryAndPrint-style
// command from 1-bit pixels, width pixels wide.
func EncodePrint(command uint16, pixels []bool, width int, printID uint32) []byte {
	pixelCount, rle := RLEFromBW(pixels, width)
	return encodePrinterMessage(command, encodePixels(pixelCount, rle), printID)
}

// EncodePersonality builds the binary_payload for SetPersonality
// (0x0102, port of encoders.py's SetPersonality branch): the face,
// nothing-to-print, cannot-see-bridge and cannot-see-internet images,
// concatenated. The printer stores these and uses them on every print.
func EncodePersonality(face, nothing, noBridge, noInternet []bool, width int, printID uint32) []byte {
	var payload []byte
	for _, pix := range [][]bool{face, nothing, noBridge, noInternet} {
		pixelCount, rle := RLEFromBW(pix, width)
		payload = append(payload, encodePixels(pixelCount, rle)...)
	}
	return encodePrinterMessage(CmdSetPersonality, payload, printID)
}

// PadWidth pads 1-bit pixels with white columns up to target width.
// Personality images are expected to span the full paper width; uploads
// narrower than that are padded rather than rejected.
func PadWidth(pix []bool, width, target int) ([]bool, int) {
	if width >= target {
		return pix, width
	}
	out := make([]bool, 0, len(pix)/width*target)
	for row := 0; row*width < len(pix); row++ {
		out = append(out, pix[row*width:(row+1)*width]...)
		out = append(out, make([]bool, target-width)...)
	}
	return out, target
}

// EncodePersonalityWithMessage builds SetPersonalityWithMessage (0x0101):
// the personality images followed by a single message.
func EncodePersonalityWithMessage(face, nothing, noBridge, noInternet, message []bool, width int, printID uint32) []byte {
	var payload []byte
	for _, pix := range [][]bool{face, nothing, noBridge, noInternet, message} {
		pixelCount, rle := RLEFromBW(pix, width)
		payload = append(payload, encodePixels(pixelCount, rle)...)
	}
	return encodePrinterMessage(CmdSetPersonalityWithMessage, payload, printID)
}
