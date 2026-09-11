package marker

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

// The worked example from docs/format.md.
const exampleBodyHex = "01 00 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51"

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func exampleMarker() Marker {
	return Marker{
		TimeSource: TimeSend,
		OriginTime: time.Date(2026, 9, 11, 21, 0, 0, 0, time.UTC),
		Sequence:   0,
		StreamID:   [8]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51},
	}
}

func TestDecodeExample(t *testing.T) {
	got, err := Decode(unhex(t, exampleBodyHex))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := exampleMarker()
	if got.TimeSource != want.TimeSource || !got.OriginTime.Equal(want.OriginTime) ||
		got.Sequence != want.Sequence || got.StreamID != want.StreamID || got.Payload != nil {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.OriginTime.Location() != time.UTC {
		t.Fatalf("OriginTime location %v, want UTC", got.OriginTime.Location())
	}
}

func TestDecodeCaptureWithPayload(t *testing.T) {
	body := unhex(t, "01 03 00 06 5b 3b 5e 16 94 00 00 00 00 07 9f 3c 1a 77 e2 b0 4d 51 00 05 68 65 6c 6c 6f")
	got, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.TimeSource != TimeCapture {
		t.Errorf("TimeSource = %v, want capture", got.TimeSource)
	}
	if got.Sequence != 7 {
		t.Errorf("Sequence = %d, want 7", got.Sequence)
	}
	if string(got.Payload) != "hello" {
		t.Errorf("Payload = %q, want hello", got.Payload)
	}
	body[24] = 'X' // the marker must not alias the input
	if string(got.Payload) != "hello" {
		t.Errorf("Payload aliases input: %q", got.Payload)
	}
}

func TestDecodeEmptyPayloadIsPresent(t *testing.T) {
	got, err := Decode(unhex(t, "01 02 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51 00 00"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Payload == nil || len(got.Payload) != 0 {
		t.Fatalf("Payload = %v, want empty non-nil", got.Payload)
	}
}

func TestDecodeIgnoresReservedBitsAndTrailingBytes(t *testing.T) {
	body := append(unhex(t, exampleBodyHex), 0xde, 0xad)
	body[1] = 0xfc // reserved bits set, time and payload flags clear
	got, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.TimeSource != TimeSend || got.Payload != nil {
		t.Fatalf("reserved bits changed the result: %+v", got)
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{"short", "01 00 00 06 5b 3b 5e 16 94 00", ErrTruncated},
		{"empty", "", ErrTruncated},
		{"version 2", "02 00 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51", ErrUnsupportedVersion},
		{"payload flag without length", "01 02 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51", ErrTruncated},
		{"payload length overruns", "01 02 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51 00 10 61 62 63", ErrTruncated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(unhex(t, tt.body))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIsFormatUUID(t *testing.T) {
	if !IsFormatUUID(FormatUUID[:]) {
		t.Error("FormatUUID not recognised")
	}
	if IsFormatUUID(FormatUUID[:15]) {
		t.Error("short slice recognised")
	}
	other := FormatUUID
	other[0]++
	if IsFormatUUID(other[:]) {
		t.Error("different UUID recognised")
	}
}

func TestTimeSourceString(t *testing.T) {
	if TimeSend.String() != "send" || TimeCapture.String() != "capture" {
		t.Fatalf("String() = %q, %q", TimeSend, TimeCapture)
	}
}
