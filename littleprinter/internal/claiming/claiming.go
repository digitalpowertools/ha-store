// Package claiming ports the parts of sirius/coding/claiming.py and
// bitshuffle.py needed to derive a device link key from a claim code.
//
// Reference vectors come from sirius's own tests:
//
//	claim code "6xwh-441j-8115-zyrh" -> link key "F7D9bmztHV32+WJScGZR0g=="
package claiming

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

// CLAIM_CODE_SALT from claiming.py.
var claimCodeSalt = []byte{
	0x38, 0x96, 0x10, 0xd9,
	0xb6, 0xb1, 0x0d, 0x16,
	0x9e, 0xe9, 0xbf, 0x87,
	0x95, 0x32, 0x62, 0x5b,
}

// Base32 digit map from claiming.py. Digits follow CC_ENCODE_LIST
// ("0-9bcdefghjkmnopqrstvwxyz"): 'A' is omitted, I/L alias '1' and U
// aliases 'V' (misread tolerance), so they don't occupy digit slots.
var base32Dict = func() map[byte]int {
	d := map[byte]int{}
	for i := 0; i < len(encodeList); i++ {
		d[encodeList[i]-'a'+'A'] = i
		d[encodeList[i]] = i
	}
	// Canonicalize() maps I/L/U away before unpack, but accept them here
	// too for robustness (same aliases as claiming.py's dict).
	d['I'] = 0x01
	d['L'] = 0x01
	d['U'] = 0x1B
	return d
}()

// encodeList from claiming.py (chars emitted by Encode, in order).
const encodeList = "0123456789bcdefghjkmnopqrstvwxyz"

// Canonicalize mirrors claiming.canonicalize: upper-case, strip dashes,
// map I/L -> 1 and U -> V (common mis-reads of the printed code).
func Canonicalize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		switch c {
		case 'L', 'I':
			c = '1'
		case 'U':
			c = 'V'
		}
		out = append(out, c)
	}
	return string(out)
}

type InvalidClaimCode struct{ Msg string }

func (e *InvalidClaimCode) Error() string { return "invalid claim code: " + e.Msg }

// unpack mirrors claiming.unpack_claim_code. Returns
// (device 24-bit, secret 40-bit, crc 16-bit).
func unpack(claimCode string) (device, secret, crc uint64, err error) {
	clean := Canonicalize(claimCode)
	if len(clean) != 16 {
		return 0, 0, 0, &InvalidClaimCode{"claim codes should be 16 characters"}
	}

	value := new(big.Int)
	pow := new(big.Int)
	thirtyTwo := big.NewInt(32)
	for i := 0; i < len(clean); i++ {
		c := clean[i]
		if clean[i] >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		d, ok := base32Dict[c]
		if !ok {
			return 0, 0, 0, &InvalidClaimCode{fmt.Sprintf("%q is not a valid character", clean[i])}
		}
		pow.Exp(thirtyTwo, big.NewInt(int64(15-i)), nil)
		value.Add(value, pow.Mul(pow, big.NewInt(int64(d))))
	}

	// device = value & 0xffffff
	// secret = (value >> 24) & 0xffffffffff
	// crc    = value >> 64
	mask24 := new(big.Int).SetUint64(0xffffff)
	mask40 := new(big.Int).SetUint64(0xffffffffff)
	dev := new(big.Int).And(value, mask24)
	sec := new(big.Int).Rsh(value, 24)
	sec.And(sec, mask40)
	c := new(big.Int).Rsh(value, 64)

	return dev.Uint64(), sec.Uint64(), c.Uint64(), nil
}

// crc16 ports crc16.py (CCITT table, init 0xffff).
func crc16(buf []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range buf {
		crc = (crc<<8) ^ ccitt16[(uint16(crc)>>8)^uint16(b)]
	}
	return crc
}

// generateLinkKey ports claiming.generate_link_key: a Wireshark-inspired
// Davies-Meyer style compression of the salt and the padded secret.
func generateLinkKey(input []byte) ([]byte, error) {
	inputLength := len(input) + len(claimCodeSalt)

	if len(input) <= 5 {
		// Pad: input + 0x80 + zeros, then the bit length as 16-bit big-endian.
		padded := make([]byte, 0, 16)
		padded = append(padded, input...)
		padded = append(padded, 0x80)
		for len(padded) < 14 {
			padded = append(padded, 0x00)
		}
		padded = append(padded, byte((inputLength*8)>>8)&0xff)
		padded = append(padded, byte((inputLength*8)>>0)&0xff)
		if len(padded) != 16 {
			return nil, fmt.Errorf("internal padding error: got %d bytes", len(padded))
		}
		input = padded
	} else if inputLength != 16 {
		return nil, &InvalidClaimCode{fmt.Sprintf("cannot handle input size %d", inputLength)}
	}

	output := make([]byte, 16)
	for _, block := range [][]byte{claimCodeSalt, input} {
		// NB: a fresh AES instance per round — the key is the *current*
		// output (Davies-Meyer style compression, as in Wireshark's
		// packet-zbee-security dissector).
		cipher, err := aes.NewCipher(output)
		if err != nil {
			return nil, err
		}
		h := make([]byte, 16)
		cipher.Encrypt(h, block)
		for i := range output {
			output[i] = h[i] ^ block[i]
		}
	}
	return output, nil
}

// Process mirrors claiming.process_claim_code: validates the CRC and
// returns (hardware_xor, base64 link key).
func Process(claimCode string) (hardwareXor uint64, linkKeyB64 string, err error) {
	device, secret, crc, err := unpack(claimCode)
	if err != nil {
		return 0, "", err
	}

	// CRC is computed over the low 64 bits of the 80-bit value
	// (device | secret<<24), packed little endian.
	low := make([]byte, 8)
	low[0] = byte(device)
	low[1] = byte(device >> 8)
	low[2] = byte(device >> 16)
	s := secret
	for i := 3; i < 8; i++ {
		low[i] = byte(s & 0xff)
		s >>= 8
	}

	serverCRC := crc16(low)
	if uint64(serverCRC) != crc {
		return 0, "", &InvalidClaimCode{"CRC problem"}
	}

	// pack("<Q", secret)[:5]: little endian 40-bit secret.
	packedSecret := make([]byte, 5)
	s2 := secret
	for i := 0; i < 5; i++ {
		packedSecret[i] = byte(s2 & 0xff)
		s2 >>= 8
	}

	linkKey, err := generateLinkKey(packedSecret)
	if err != nil {
		return 0, "", err
	}
	return device, base64.StdEncoding.EncodeToString(linkKey), nil
}

// KeyFromClaimCode mirrors claiming.key_from_claim_code.
func KeyFromClaimCode(claimCode string) (string, error) {
	_, key, err := Process(claimCode)
	return key, err
}

// HardwareXorFromDeviceAddress ports bitshuffle.hardware_xor_from_device_address.
func HardwareXorFromDeviceAddress(deviceAddress string) (uint64, error) {
	b, err := hex.DecodeString(padHex(deviceAddress))
	if err != nil {
		return 0, fmt.Errorf("bad device address %q: %v", deviceAddress, err)
	}
	if len(b) != 8 {
		return 0, fmt.Errorf("device address %q must be 8 bytes", deviceAddress)
	}
	// reverse (little endian)
	for i, j := 0, 7; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	c0 := b[0] ^ b[5]
	c1 := b[1] ^ b[3] ^ b[6]
	c2 := b[2] ^ b[4] ^ b[7]
	return uint64(c2)<<16 | uint64(c1)<<8 | uint64(c0), nil
}

func padHex(s string) string {
	if len(s)%2 == 1 {
		return "0" + s
	}
	return s
}

// ccitt16 is the table from crc16.py.
var ccitt16 = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50A5, 0x60C6, 0x70E7,
	0x8108, 0x9129, 0xA14A, 0xB16B, 0xC18C, 0xD1AD, 0xE1CE, 0xF1EF,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52B5, 0x4294, 0x72F7, 0x62D6,
	0x9339, 0x8318, 0xB37B, 0xA35A, 0xD3BD, 0xC39C, 0xF3FF, 0xE3DE,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64E6, 0x74C7, 0x44A4, 0x5485,
	0xA56A, 0xB54B, 0x8528, 0x9509, 0xE5EE, 0xF5CF, 0xC5AC, 0xD58D,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76D7, 0x66F6, 0x5695, 0x46B4,
	0xB75B, 0xA77A, 0x9719, 0x8738, 0xF7DF, 0xE7FE, 0xD79D, 0xC7BC,
	0x48C4, 0x58E5, 0x6886, 0x78A7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xC9CC, 0xD9ED, 0xE98E, 0xF9AF, 0x8948, 0x9969, 0xA90A, 0xB92B,
	0x5AF5, 0x4AD4, 0x7AB7, 0x6A96, 0x1A71, 0x0A50, 0x3A33, 0x2A12,
	0xDBFD, 0xCBDC, 0xFBBF, 0xEB9E, 0x9B79, 0x8B58, 0xBB3B, 0xAB1A,
	0x6CA6, 0x7C87, 0x4CE4, 0x5CC5, 0x2C22, 0x3C03, 0x0C60, 0x1C41,
	0xEDAE, 0xFD8F, 0xCDEC, 0xDDCD, 0xAD2A, 0xBD0B, 0x8D68, 0x9D49,
	0x7E97, 0x6EB6, 0x5ED5, 0x4EF4, 0x3E13, 0x2E32, 0x1E51, 0x0E70,
	0xFF9F, 0xEFBE, 0xDFDD, 0xCFFC, 0xBF1B, 0xAF3A, 0x9F59, 0x8F78,
	0x9188, 0x81A9, 0xB1CA, 0xA1EB, 0xD10C, 0xC12D, 0xF14E, 0xE16F,
	0x1080, 0x00A1, 0x30C2, 0x20E3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83B9, 0x9398, 0xA3FB, 0xB3DA, 0xC33D, 0xD31C, 0xE37F, 0xF35E,
	0x02B1, 0x1290, 0x22F3, 0x32D2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xB5EA, 0xA5CB, 0x95A8, 0x8589, 0xF56E, 0xE54F, 0xD52C, 0xC50D,
	0x34E2, 0x24C3, 0x14A0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xA7DB, 0xB7FA, 0x8799, 0x97B8, 0xE75F, 0xF77E, 0xC71D, 0xD73C,
	0x26D3, 0x36F2, 0x0691, 0x16B0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xD94C, 0xC96D, 0xF90E, 0xE92F, 0x99C8, 0x89E9, 0xB98A, 0xA9AB,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18C0, 0x08E1, 0x3882, 0x28A3,
	0xCB7D, 0xDB5C, 0xEB3F, 0xFB1E, 0x8BF9, 0x9BD8, 0xABBB, 0xBB9A,
	0x4A75, 0x5A54, 0x6A37, 0x7A16, 0x0AF1, 0x1AD0, 0x2AB3, 0x3A92,
	0xFD2E, 0xED0F, 0xDD6C, 0xCD4D, 0xBDAA, 0xAD8B, 0x9DE8, 0x8DC9,
	0x7C26, 0x6C07, 0x5C64, 0x4C45, 0x3CA2, 0x2C83, 0x1CE0, 0x0CC1,
	0xEF1F, 0xFF3E, 0xCF5D, 0xDF7C, 0xAF9B, 0xBFBA, 0x8FD9, 0x9FF8,
	0x6E17, 0x7E36, 0x4E55, 0x5E74, 0x2E93, 0x3EB2, 0x0ED1, 0x1EF0,
}
