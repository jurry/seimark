package h264

import (
	"errors"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		au   []byte
		want Format
	}{
		{"4-byte start code", []byte{0, 0, 0, 1, 0x65, 0x88}, FormatAnnexB},
		{"3-byte start code", []byte{0, 0, 1, 0x65, 0x88}, FormatAnnexB},
		{"length prefixed", []byte{0, 0, 0, 2, 0x65, 0x88}, FormatLengthPrefixed},
		{"length too long", []byte{0, 0, 0, 9, 0x65, 0x88}, FormatUnknown},
		{"zero length", []byte{0, 0, 0, 0, 0x65, 0x88}, FormatUnknown},
		{"too short", []byte{0, 0}, FormatUnknown},
		{"empty", nil, FormatUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DetectFormat(tt.au); got != tt.want {
				t.Fatalf("DetectFormat = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNALUnitsAnnexB(t *testing.T) {
	t.Parallel()

	au := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 1, 0x68, 0xce, 0, 0, 0, 1, 0x65, 0x88, 0x84}

	nalus, err := NALUnits(au, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}

	if len(nalus) != 3 || nalus[0][0] != 0x67 || nalus[1][0] != 0x68 || nalus[2][0] != 0x65 {
		t.Fatalf("got %x", nalus)
	}

	if len(nalus[2]) != 3 {
		t.Fatalf("last NAL unit length %d, want 3", len(nalus[2]))
	}
}

func TestNALUnitsLengthPrefixed(t *testing.T) {
	t.Parallel()

	au := []byte{0, 0, 0, 2, 0x67, 0x42, 0, 0, 0, 3, 0x65, 0x88, 0x84}

	nalus, err := NALUnits(au, FormatLengthPrefixed)
	if err != nil {
		t.Fatal(err)
	}

	if len(nalus) != 2 || nalus[0][0] != 0x67 || len(nalus[1]) != 3 {
		t.Fatalf("got %x", nalus)
	}
}

func TestNALUnitsLengthOverrun(t *testing.T) {
	t.Parallel()

	au := []byte{0, 0, 0, 9, 0x65, 0x88}
	if _, err := NALUnits(au, FormatLengthPrefixed); err == nil {
		t.Fatal("overrunning length accepted")
	}
}

func TestNALUnitsUnknownFormat(t *testing.T) {
	t.Parallel()

	if _, err := NALUnits([]byte{1, 2, 3}, FormatUnknown); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("err = %v, want ErrUnknownFormat", err)
	}
}

func TestNALUnitsCorruptLengthIsAnError(t *testing.T) {
	t.Parallel()

	au := []byte{0, 0, 0, 4, 6, 1, 2, 3, 0xff, 0xff, 0xff, 0xfd, 6, 6}
	if _, err := Markers(au, FormatLengthPrefixed); err == nil {
		t.Fatal("corrupt length accepted")
	}
}
