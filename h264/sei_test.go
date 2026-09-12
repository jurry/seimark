package h264

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jurry/seimark/marker"
)

// The worked example from docs/format.md.
const (
	exampleBodyHex = "01 00 00 06 5b 4f 7b ed f4 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51"
	exampleNALHex  = "06 05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a 01 00 00 06 5b 4f 7b ed f4 00 00 03 00 00 03 00 9f 3c 1a 77 e2 b0 4d 51 80"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestUserDataSEINALMatchesSpecExample(t *testing.T) {
	t.Parallel()
	got, err := UserDataSEINAL(marker.FormatUUID(), unhex(t, exampleBodyHex))
	if err != nil {
		t.Fatal(err)
	}
	if want := unhex(t, exampleNALHex); !bytes.Equal(got, want) {
		t.Fatalf("got  %x\nwant %x", got, want)
	}
}

func TestUserDataSEINALLargeBodyUsesMultiBytePayloadSize(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte{0x11}, 300) // payloadSize 316 = 0xff + 0x3d
	got, err := UserDataSEINAL(marker.FormatUUID(), body)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0x06 || got[1] != 0x05 || got[2] != 0xff || got[3] != 0x3d {
		t.Fatalf("header bytes %x, want 06 05 ff 3d", got[:4])
	}
	if got[len(got)-1] != 0x80 {
		t.Fatalf("last byte %x, want 80", got[len(got)-1])
	}
}

func annexB(nalus ...[]byte) []byte {
	var out []byte
	for _, n := range nalus {
		out = append(out, 0, 0, 0, 1)
		out = append(out, n...)
	}
	return out
}

func lengthPrefixed(nalus ...[]byte) []byte {
	var out []byte
	for _, n := range nalus {
		out = append(out, byte(len(n)>>24), byte(len(n)>>16), byte(len(n)>>8), byte(len(n)))
		out = append(out, n...)
	}
	return out
}

var (
	sps    = []byte{0x67, 0x42, 0xc0, 0x0d, 0xda, 0x05, 0x82, 0x51}
	pps    = []byte{0x68, 0xce, 0x38, 0x80}
	idr    = []byte{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}
	nonIDR = []byte{0x41, 0x9a, 0x22, 0x0c, 0x7f}
)

func exampleMarkerNAL(t *testing.T) []byte {
	t.Helper()
	return unhex(t, exampleNALHex)
}

func foreignSEINAL(t *testing.T) []byte {
	t.Helper()
	var uuid [16]byte
	copy(uuid[:], "x264-user-data!!")
	nal, err := UserDataSEINAL(uuid, []byte("x264 - core 164 r3108"))
	if err != nil {
		t.Fatal(err)
	}
	return nal
}

func TestMarkersFindsOneBeforeVCL(t *testing.T) {
	t.Parallel()
	au := annexB(sps, pps, exampleMarkerNAL(t), idr)
	got, err := Markers(au, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Sequence != 0 || got[0].StreamID != [8]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51} {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkersLengthPrefixedAndAppended(t *testing.T) {
	t.Parallel()
	au := lengthPrefixed(nonIDR, exampleMarkerNAL(t))
	got, err := Markers(au, FormatLengthPrefixed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d markers, want 1", len(got))
	}
}

func TestMarkersSkipsForeignSEI(t *testing.T) {
	t.Parallel()
	au := annexB(foreignSEINAL(t), exampleMarkerNAL(t), idr)
	got, err := Markers(au, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d markers, want 1", len(got))
	}
}

func TestMarkersTwoInOneAccessUnitKeepOrder(t *testing.T) {
	t.Parallel()
	second := marker.Marker{OriginTime: time.Unix(1789246800, 0), Sequence: 9, StreamID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	body, err := second.Encode()
	if err != nil {
		t.Fatal(err)
	}
	secondNAL, err := UserDataSEINAL(marker.FormatUUID(), body)
	if err != nil {
		t.Fatal(err)
	}
	au := annexB(exampleMarkerNAL(t), secondNAL, idr)
	got, err := Markers(au, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Sequence != 0 || got[1].Sequence != 9 {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkersNoSEI(t *testing.T) {
	t.Parallel()
	got, err := Markers(annexB(sps, pps, idr), FormatAnnexB)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
}

func TestMarkersMalformedMarkerIsAnError(t *testing.T) {
	t.Parallel()
	bad, err := UserDataSEINAL(marker.FormatUUID(), []byte{0x02, 0x00, 0x00}) // version 2, truncated
	if err != nil {
		t.Fatal(err)
	}
	au := annexB(exampleMarkerNAL(t), bad, idr)
	got, err := Markers(au, FormatAnnexB)
	if !errors.Is(err, marker.ErrUnsupportedVersion) && !errors.Is(err, marker.ErrTruncated) {
		t.Fatalf("err = %v, want a marker error", err)
	}
	if len(got) != 1 {
		t.Fatalf("markers found before the error: %d, want 1", len(got))
	}
}

func TestMarkersUnparsableSEIIsReported(t *testing.T) {
	t.Parallel()
	garbage := []byte{0x06, 0xff, 0xff, 0xff} // SEI header, then a payload type that never ends
	got, err := Markers(annexB(garbage, exampleMarkerNAL(t), idr), FormatAnnexB)
	if !errors.Is(err, ErrUnparsableSEI) {
		t.Fatalf("err = %v, want ErrUnparsableSEI", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d markers, want 1", len(got))
	}
}
