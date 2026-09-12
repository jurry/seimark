package h264

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jurry/seimark/marker"
)

var (
	aud        = []byte{0x09, 0xf0}
	sliceCont  = []byte{0x41, 0x1a, 0x22} // first_mb_in_slice != 0: continues the picture
	nonIDRMore = []byte{0x41, 0x9a, 0x33}
)

func collect(t *testing.T, stream []byte) [][]byte {
	t.Helper()

	var out [][]byte

	for au, err := range AccessUnits(bytes.NewReader(stream)) {
		if err != nil {
			t.Fatalf("AccessUnits: %v", err)
		}

		out = append(out, au)
	}

	return out
}

func TestAccessUnitsSplitsOnNewPicture(t *testing.T) {
	t.Parallel()

	stream := annexB(sps, pps, idr, nonIDR, nonIDRMore)

	aus := collect(t, stream)
	if len(aus) != 3 {
		t.Fatalf("got %d access units, want 3", len(aus))
	}

	first, _ := NALUnits(aus[0], FormatAnnexB)
	if len(first) != 3 || first[0][0] != 0x67 || first[2][0] != 0x65 {
		t.Fatalf("first access unit %x", first)
	}
}

func TestAccessUnitsKeepsSecondSliceOfSamePicture(t *testing.T) {
	t.Parallel()

	aus := collect(t, annexB(idr, sliceCont, nonIDR))
	if len(aus) != 2 {
		t.Fatalf("got %d access units, want 2", len(aus))
	}

	n, _ := NALUnits(aus[0], FormatAnnexB)
	if len(n) != 2 {
		t.Fatalf("first access unit has %d NAL units, want 2", len(n))
	}
}

func TestAccessUnitsSplitsOnDelimiterAndParameterSets(t *testing.T) {
	t.Parallel()

	aus := collect(t, annexB(aud, sps, pps, idr, aud, nonIDR, sps, pps, idr))
	if len(aus) != 3 {
		t.Fatalf("got %d access units, want 3", len(aus))
	}
}

func TestAccessUnitsMarkerBeforeVCLStaysWithItsPicture(t *testing.T) {
	t.Parallel()
	m := exampleMarkerNAL(t)

	aus := collect(t, annexB(sps, pps, m, idr, m, nonIDR))
	if len(aus) != 2 {
		t.Fatalf("got %d access units, want 2", len(aus))
	}

	for i, au := range aus {
		got, err := Markers(au, FormatAnnexB)
		if err != nil || len(got) != 1 {
			t.Fatalf("access unit %d: markers %v, err %v", i, got, err)
		}
	}
}

func TestAccessUnitsThreeByteStartCodesAndNoTrailingStartCode(t *testing.T) {
	t.Parallel()

	stream := make([]byte, 0, 3+len(idr)+3+len(nonIDR))
	stream = append(stream, 0, 0, 1)
	stream = append(stream, idr...)
	stream = append(stream, 0, 0, 1)
	stream = append(stream, nonIDR...)

	aus := collect(t, stream)
	if len(aus) != 2 {
		t.Fatalf("got %d access units, want 2", len(aus))
	}

	n, _ := NALUnits(aus[1], FormatAnnexB)
	if len(n) != 1 || !bytes.Equal(n[0], nonIDR) {
		t.Fatalf("last access unit %x, want %x", n, nonIDR)
	}
}

func TestAccessUnitsIgnoresLeadingGarbageAndEmptyInput(t *testing.T) {
	t.Parallel()

	stream := append([]byte{0xaa, 0xbb}, annexB(idr)...)
	if aus := collect(t, stream); len(aus) != 1 {
		t.Fatalf("got %d access units, want 1", len(aus))
	}

	if aus := collect(t, nil); len(aus) != 0 {
		t.Fatalf("got %d access units from empty input, want 0", len(aus))
	}
}

type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n == 0 {
		return 0, errors.New("boom")
	}

	f.n--

	copy(p, annexB(idr))

	return len(annexB(idr)), nil
}

func TestAccessUnitsReportsReadError(t *testing.T) {
	t.Parallel()

	var sawErr bool

	for _, err := range AccessUnits(&failingReader{n: 1}) {
		if err != nil {
			sawErr = true
		}
	}

	if !sawErr {
		t.Fatal("read error not reported")
	}
}

func TestAccessUnitsStopsWhenConsumerStops(t *testing.T) {
	t.Parallel()

	count := 0
	for range AccessUnits(bytes.NewReader(annexB(idr, nonIDR, nonIDRMore))) {
		count++
		break
	}

	if count != 1 {
		t.Fatalf("yielded %d after break", count)
	}
}

// markerNALFor builds a marker SEI NAL unit with the given sequence number.
func markerNALFor(t *testing.T, seq uint32) []byte {
	t.Helper()

	body, err := marker.Marker{
		OriginTime: time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC),
		Sequence:   seq,
		StreamID:   [8]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51},
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}

	nal, err := UserDataSEINAL(marker.FormatUUID(), body)
	if err != nil {
		t.Fatal(err)
	}

	return nal
}

// appendPlaced rewrites the Annex B fixture so every marker SEI sits after the
// slice NAL unit of its access unit instead of before it.
func appendPlaced(t *testing.T) []byte {
	t.Helper()

	in, err := os.Open(filepath.Join("..", "vectors", "streams", "testsrc-marked.h264"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = in.Close() }()

	var out []byte

	units := 0

	for au, err := range AccessUnits(in) {
		if err != nil {
			t.Fatal(err)
		}

		nalus, err := NALUnits(au, FormatAnnexB)
		if err != nil {
			t.Fatal(err)
		}

		var (
			markerNAL []byte
			rest      [][]byte
		)

		for _, nal := range nalus {
			if ms, _ := Markers(annexB(nal), FormatAnnexB); len(ms) > 0 {
				markerNAL = nal
				continue
			}

			rest = append(rest, nal)
		}

		if markerNAL == nil {
			t.Fatalf("access unit %d has no marker", units)
		}

		out = append(out, annexB(append(rest, markerNAL)...)...)
		units++
	}

	if units != 20 {
		t.Fatalf("fixture has %d access units, want 20", units)
	}

	return out
}

func TestAccessUnitsAppendPlacedMarkerStaysWithItsPicture(t *testing.T) {
	t.Parallel()

	aus := collect(t, appendPlaced(t))
	if len(aus) != 20 {
		t.Fatalf("got %d access units, want 20", len(aus))
	}

	for i, au := range aus {
		got, err := Markers(au, FormatAnnexB)
		if err != nil {
			t.Fatalf("access unit %d: %v", i, err)
		}

		if len(got) != 1 {
			t.Fatalf("access unit %d has %d markers, want 1", i, len(got))
		}

		if got[0].Sequence != uint32(i) {
			t.Errorf("access unit %d: sequence %d", i, got[0].Sequence)
		}
	}
}

func TestAccessUnitsSecondAppendedMarkerStartsANewUnit(t *testing.T) {
	t.Parallel()
	// Documented limit: an access unit carries one append-placed marker; a
	// second one is read as the start of the next access unit.
	aus := collect(t, annexB(idr, markerNALFor(t, 0), markerNALFor(t, 1)))
	if len(aus) != 2 {
		t.Fatalf("got %d access units, want 2", len(aus))
	}

	first, err := Markers(aus[0], FormatAnnexB)
	if err != nil || len(first) != 1 || first[0].Sequence != 0 {
		t.Fatalf("first unit: %v, %v", first, err)
	}

	second, err := Markers(aus[1], FormatAnnexB)
	if err != nil || len(second) != 1 || second[0].Sequence != 1 {
		t.Fatalf("second unit: %v, %v", second, err)
	}
}
