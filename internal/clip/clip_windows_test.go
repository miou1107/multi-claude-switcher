//go:build windows

package clip

import (
	"testing"
)

// TestUtf16le checks the pure encoding step that lets a report survive the
// round trip through PowerShell's -EncodedCommand and
// System.Text.Encoding.Unicode, both of which expect UTF-16LE. It is the only
// part of this package that has no I/O and so is safe to test directly.
func TestUtf16le(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []byte
	}{
		{
			name: "ascii",
			in:   "AB",
			// 'A' = U+0041, 'B' = U+0042, little-endian: low byte first.
			want: []byte{0x41, 0x00, 0x42, 0x00},
		},
		{
			name: "non-ascii BMP character",
			// é = U+00E9, still a single UTF-16 code unit.
			in:   "é",
			want: []byte{0xE9, 0x00},
		},
		{
			name: "character outside the BMP encodes as a surrogate pair",
			// 😀 = U+1F600, which needs a UTF-16 surrogate pair:
			// high surrogate 0xD83D, low surrogate 0xDE00.
			in:   "😀",
			want: []byte{0x3D, 0xD8, 0x00, 0xDE},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := utf16le(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("utf16le(%q) = % x, want % x (length mismatch)", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("utf16le(%q) = % x, want % x", tc.in, got, tc.want)
				}
			}
		})
	}
}
