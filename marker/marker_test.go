package marker

import (
	"bytes"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	got, err := Decode(unhex(t, "01 02 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51 00 00"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Payload == nil || len(got.Payload) != 0 {
		t.Fatalf("Payload = %v, want empty non-nil", got.Payload)
	}
}

func TestDecodeIgnoresReservedBitsAndTrailingBytes(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			_, err := Decode(unhex(t, tt.body))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIsFormatUUID(t *testing.T) {
	t.Parallel()
	uuid := FormatUUID()
	if !IsFormatUUID(uuid[:]) {
		t.Error("FormatUUID not recognised")
	}
	if IsFormatUUID(uuid[:15]) {
		t.Error("short slice recognised")
	}
	other := FormatUUID()
	other[0]++
	if IsFormatUUID(other[:]) {
		t.Error("different UUID recognised")
	}
}

func TestTimeSourceString(t *testing.T) {
	t.Parallel()
	if TimeSend.String() != "send" || TimeCapture.String() != "capture" {
		t.Fatalf("String() = %q, %q", TimeSend, TimeCapture)
	}
}

func TestEncodeExample(t *testing.T) {
	t.Parallel()
	got, err := exampleMarker().Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := unhex(t, exampleBodyHex); !bytes.Equal(got, want) {
		t.Fatalf("got %x\nwant %x", got, want)
	}
}

func TestEncodeCaptureWithPayload(t *testing.T) {
	t.Parallel()
	m := exampleMarker()
	m.TimeSource = TimeCapture
	m.Sequence = 7
	m.Payload = []byte("hello")
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := unhex(t, "01 03 00 06 5b 3b 5e 16 94 00 00 00 00 07 9f 3c 1a 77 e2 b0 4d 51 00 05 68 65 6c 6c 6f")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x\nwant %x", got, want)
	}
}

func TestEncodeEmptyPayloadSetsFlag(t *testing.T) {
	t.Parallel()
	m := exampleMarker()
	m.Payload = []byte{}
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got[1]&0x02 == 0 || len(got) != FixedSize+2 {
		t.Fatalf("got %x, want payload flag and a zero length", got)
	}
}

func TestEncodePayloadTooLarge(t *testing.T) {
	t.Parallel()
	m := exampleMarker()
	m.Payload = make([]byte, PayloadHardLimit+1)
	if _, err := m.Encode(); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
	m.Payload = make([]byte, PayloadHardLimit)
	if _, err := m.Encode(); err != nil {
		t.Fatalf("hard limit itself rejected: %v", err)
	}
}

func TestRoundTripRoundsToMicroseconds(t *testing.T) {
	t.Parallel()
	m := exampleMarker()
	m.OriginTime = m.OriginTime.Add(1500 * time.Nanosecond)
	body, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := m.OriginTime.Truncate(time.Microsecond); !got.OriginTime.Equal(want) {
		t.Fatalf("OriginTime = %v, want %v", got.OriginTime, want)
	}
}

func TestDecodeZeroMarkerRoundTrips(t *testing.T) {
	t.Parallel()
	body, err := Marker{}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.OriginTime.Equal(Marker{}.OriginTime) {
		t.Errorf("OriginTime = %v, want %v", got.OriginTime, Marker{}.OriginTime)
	}
	if got.Sequence != 0 || got.StreamID != ([8]byte{}) || got.Payload != nil {
		t.Errorf("got %+v, want the zero marker", got)
	}
}

func TestDecodeNegativeOriginTime(t *testing.T) {
	t.Parallel()
	want := Marker{OriginTime: time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC)}
	body, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(body)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.OriginTime.Equal(want.OriginTime) {
		t.Fatalf("OriginTime = %v, want %v", got.OriginTime, want.OriginTime)
	}
	if got.OriginTime.UnixMicro() != -1000000 {
		t.Fatalf("origin_us = %d, want -1000000", got.OriginTime.UnixMicro())
	}
}
