package claiming

import "testing"

// Golden vector from sirius/coding/test_encoders.py:
// test_add_device_encryption_key.
func TestKeyFromClaimCode(t *testing.T) {
	got, err := KeyFromClaimCode("6xwh-441j-8115-zyrh")
	if err != nil {
		t.Fatal(err)
	}
	want := "F7D9bmztHV32+WJScGZR0g=="
	if got != want {
		t.Errorf("key:\n got  %s\n want %s", got, want)
	}
}

func TestCanonicalize(t *testing.T) {
	cases := map[string]string{
		"6xwh-441j-8115-zyrh": "6XWH441J8115ZYRH",
		"6xwh441j8115zyrh":    "6XWH441J8115ZYRH",
		// Common misreads: I/L -> 1, U -> V, lowercase.
		"6xwi-441j-8115-zyru": "6XW1441J8115ZYRV",
	}
	for in, want := range cases {
		if got := Canonicalize(in); got != want {
			t.Errorf("Canonicalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInvalidClaimCode(t *testing.T) {
	for _, code := range []string{
		"",
		"too-short",
		"axwh-441j-8115-zyrh", // 'A' is omitted from the base32 alphabet
		"6xwh-441j-8115-zyrZ", // wrong length
	} {
		if _, err := KeyFromClaimCode(code); err == nil {
			t.Errorf("expected error for %q", code)
		}
	}
}

func TestHardwareXorFromDeviceAddress(t *testing.T) {
	// bitshuffle: reverse 8 address bytes, xor (b0^b5, b1^b3^b6, b2^b4^b7).
	// Address 0001020304050607 -> b=[07 06 05 04 03 02 01 00]
	//   c0 = b0^b5 = 07^02 = 05
	//   c1 = b1^b3^b6 = 06^04^01 = 03
	//   c2 = b2^b4^b7 = 05^03^00 = 06
	// result = 0x060305
	got, err := HardwareXorFromDeviceAddress("0001020304050607")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x060305 {
		t.Errorf("hardware_xor = %#x, want 0x060305", got)
	}
}
