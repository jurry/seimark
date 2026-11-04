package h264

import (
	"bytes"
	"errors"
	"slices"
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

func TestAccessUnitsSEIAfterTheLastVCLStartsTheNextUnit(t *testing.T) {
	t.Parallel()
	// The standard rule, with no marker exception: an SEI NAL unit that follows
	// a picture belongs to the next access unit, marker or not.
	aus := collect(t, annexB(idr, markerNALFor(t, 0), nonIDR))
	if len(aus) != 2 {
		t.Fatalf("got %d access units, want 2", len(aus))
	}

	first, err := Markers(aus[0], FormatAnnexB)
	if err != nil || len(first) != 0 {
		t.Fatalf("first unit: markers %v, err %v", first, err)
	}

	second, err := Markers(aus[1], FormatAnnexB)
	if err != nil || len(second) != 1 || second[0].Sequence != 0 {
		t.Fatalf("second unit: markers %v, err %v", second, err)
	}
}

func TestAccessUnitsKeyframesOnlyShape(t *testing.T) {
	t.Parallel()
	// [SPS PPS IDR][nonIDR][IDR] with only the IDR units marked, as
	// -keyframes-only writes it.
	stream := annexB(sps, pps, markerNALFor(t, 0), idr)
	stream = append(stream, annexB(nonIDR)...)
	stream = append(stream, annexB(markerNALFor(t, 1), idr)...)

	aus := collect(t, stream)
	if len(aus) != 3 {
		t.Fatalf("got %d access units, want 3", len(aus))
	}

	want := []int{1, 0, 1}

	for i, au := range aus {
		got, err := Markers(au, FormatAnnexB)
		if err != nil {
			t.Fatalf("access unit %d: %v", i, err)
		}

		if len(got) != want[i] {
			t.Fatalf("access unit %d has %d markers, want %d", i, len(got), want[i])
		}
	}

	first, _ := Markers(aus[0], FormatAnnexB)
	third, _ := Markers(aus[2], FormatAnnexB)

	if first[0].Sequence != 0 || third[0].Sequence != 1 {
		t.Fatalf("sequences %d and %d, want 0 and 1", first[0].Sequence, third[0].Sequence)
	}
}

func TestAccessUnitsAtReportsTheOffsetOfEachStartCode(t *testing.T) {
	t.Parallel()

	// lead is what precedes a unit's 00 00 01: garbage, the zero of a four-byte
	// start code, trailing zeros or empty NAL units. None of it is counted.
	type unit struct{ lead, nal []byte }

	for _, tc := range []struct {
		name  string
		units []unit
	}{
		{
			name:  "leading garbage, then four-, three- and four-byte start codes",
			units: []unit{{[]byte{0xaa, 0xbb, 0}, idr}, {nil, nonIDR}, {[]byte{0}, nonIDRMore}},
		},
		{
			name:  "four-byte start codes throughout",
			units: []unit{{[]byte{0}, idr}, {[]byte{0}, nonIDR}, {[]byte{0}, nonIDRMore}},
		},
		{
			name:  "trailing zeros before a three-byte start code",
			units: []unit{{nil, idr}, {[]byte{0, 0, 0}, nonIDR}},
		},
		{
			name:  "empty NAL units",
			units: []unit{{nil, idr}, {[]byte{0, 0, 1, 0, 0, 1}, nonIDR}, {[]byte{0, 0, 0, 1, 0}, nonIDRMore}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stream []byte

			var want []int

			for _, u := range tc.units {
				stream = append(stream, u.lead...)
				want = append(want, len(stream))
				stream = append(stream, 0, 0, 1)
				stream = append(stream, u.nal...)
			}

			var got []int

			for au, err := range AccessUnitsAt(bytes.NewReader(stream)) {
				if err != nil {
					t.Fatalf("AccessUnitsAt: %v", err)
				}

				got = append(got, au.Offset)
			}

			if !slices.Equal(got, want) {
				t.Errorf("offsets %v, want %v", got, want)
			}
		})
	}
}

func TestAccessUnitsSkipsEmptyNALUnits(t *testing.T) {
	t.Parallel()

	// Start codes with nothing between them enclose no NAL unit; they must
	// neither appear as one nor end the scan.
	for _, tc := range []struct {
		name   string
		stream []byte
		want   [][][]byte // the NAL units of each access unit
	}{
		{
			name:   "four-byte start codes",
			stream: slices.Concat(annexB(sps, pps, idr), []byte{0, 0, 0, 1, 0, 0, 0, 1}, annexB(nonIDR, nonIDRMore)),
			want:   [][][]byte{{sps, pps, idr}, {nonIDR}, {nonIDRMore}},
		},
		{
			name:   "three-byte start codes",
			stream: slices.Concat(annexB(idr), []byte{0, 0, 1, 0, 0, 1}, annexB(nonIDR)),
			want:   [][][]byte{{idr}, {nonIDR}},
		},
		{
			name:   "a bare start code at the end",
			stream: append(annexB(idr, nonIDR), 0, 0, 0, 1),
			want:   [][][]byte{{idr}, {nonIDR}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			aus := collect(t, tc.stream)
			if len(aus) != len(tc.want) {
				t.Fatalf("got %d access units, want %d", len(aus), len(tc.want))
			}

			for i, au := range aus {
				nals, err := NALUnits(au, FormatAnnexB)
				if err != nil {
					t.Fatalf("access unit %d: NALUnits: %v", i, err)
				}

				if !slices.EqualFunc(nals, tc.want[i], bytes.Equal) {
					t.Errorf("access unit %d is %x, want %x", i, nals, tc.want[i])
				}
			}
		})
	}
}
