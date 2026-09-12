package h264

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/jurry/seimark/marker"
)

var (
	testStreamID = [marker.StreamIDSize]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51}
	t0           = time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC)
)

func newTestWriter(t *testing.T, opts WriterOptions) *Writer {
	t.Helper()

	if opts.StreamID == [marker.StreamIDSize]byte{} {
		opts.StreamID = testStreamID
	}

	w, err := NewWriter(opts)
	if err != nil {
		t.Fatal(err)
	}

	return w
}

func naluTypes(t *testing.T, au []byte, f Format) []int {
	t.Helper()

	nalus, err := NALUnits(au, f)
	if err != nil {
		t.Fatal(err)
	}

	types := make([]int, 0, len(nalus))
	for _, n := range nalus {
		types = append(types, int(n[0]&0x1f))
	}

	return types
}

func TestMarkBeforeVCLWithParameterSets(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, marked, err := w.Mark(annexB(sps, pps, idr), FormatAnnexB, t0, nil)
	if err != nil || !marked {
		t.Fatalf("Mark: marked=%v err=%v", marked, err)
	}

	if got, want := naluTypes(t, out, FormatAnnexB), []int{7, 8, 6, 5}; !equalInts(got, want) {
		t.Fatalf("NAL types %v, want %v", got, want)
	}

	ms, err := Markers(out, FormatAnnexB)
	if err != nil || len(ms) != 1 {
		t.Fatalf("Markers: %v %v", ms, err)
	}

	if ms[0].Sequence != 0 || !ms[0].OriginTime.Equal(t0) || ms[0].StreamID != testStreamID || ms[0].Payload != nil || ms[0].TimeSource != marker.TimeSend {
		t.Fatalf("marker %+v", ms[0])
	}

	if w.Sequence() != 1 {
		t.Fatalf("Sequence() = %d after one mark, want 1", w.Sequence())
	}

	out2, _, err := w.Mark(annexB(nonIDR), FormatAnnexB, t0.Add(100*time.Millisecond), nil)
	if err != nil {
		t.Fatal(err)
	}

	ms2, _ := Markers(out2, FormatAnnexB)
	if len(ms2) != 1 || ms2[0].Sequence != 1 {
		t.Fatalf("second marker %+v", ms2)
	}
}

func TestMarkGoesAfterExistingSEI(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, _, err := w.Mark(annexB(foreignSEINAL(t), idr), FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	nalus, _ := NALUnits(out, FormatAnnexB)
	if len(nalus) != 3 || !carriesMarker(nalus[1]) || carriesMarker(nalus[0]) {
		t.Fatalf("expected foreign SEI, marker SEI, IDR; got types %v", naluTypes(t, out, FormatAnnexB))
	}
}

func TestMarkAppend(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{Placement: PlacementAppend})

	out, _, err := w.Mark(annexB(sps, pps, idr), FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := naluTypes(t, out, FormatAnnexB), []int{7, 8, 5, 6}; !equalInts(got, want) {
		t.Fatalf("NAL types %v, want %v", got, want)
	}
}

func TestMarkLengthPrefixedStaysLengthPrefixed(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, _, err := w.Mark(lengthPrefixed(sps, pps, idr), FormatLengthPrefixed, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if DetectFormat(out) != FormatLengthPrefixed {
		t.Fatalf("output format %v", DetectFormat(out))
	}

	ms, err := Markers(out, FormatLengthPrefixed)
	if err != nil || len(ms) != 1 {
		t.Fatalf("Markers: %v %v", ms, err)
	}
}

func TestMarkDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})
	in := annexB(sps, pps, idr)

	out, _, err := w.Mark(in, FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := append([]byte{}, out...)

	for i := range in {
		in[i] = 0xee
	}

	if !bytes.Equal(out, snapshot) {
		t.Fatal("output changed when the input was overwritten")
	}
}

func TestMarkKeyframesOnly(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{KeyframesOnly: true})
	in := annexB(nonIDR)

	out, marked, err := w.Mark(in, FormatAnnexB, t0, nil)
	if err != nil || marked || !bytes.Equal(out, in) || w.Sequence() != 0 {
		t.Fatalf("non-IDR unit: marked=%v err=%v equal=%v seq=%d", marked, err, bytes.Equal(out, in), w.Sequence())
	}

	_, marked, err = w.Mark(annexB(sps, pps, idr), FormatAnnexB, t0, nil)
	if err != nil || !marked || w.Sequence() != 1 {
		t.Fatalf("IDR unit: marked=%v err=%v seq=%d", marked, err, w.Sequence())
	}
}

func TestMarkAlreadyMarked(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, _, err := w.Mark(annexB(idr), FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, marked, err := w.Mark(out, FormatAnnexB, t0, nil)
	if !errors.Is(err, ErrAlreadyMarked) || marked || w.Sequence() != 1 {
		t.Fatalf("second mark: marked=%v err=%v seq=%d", marked, err, w.Sequence())
	}
}

func TestMarkPayloadLimits(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	_, marked, err := w.Mark(annexB(idr), FormatAnnexB, t0, make([]byte, marker.PayloadHardLimit+1))
	if !errors.Is(err, marker.ErrPayloadTooLarge) || marked || w.Sequence() != 0 {
		t.Fatalf("hard limit: marked=%v err=%v seq=%d", marked, err, w.Sequence())
	}

	out, marked, err := w.Mark(annexB(idr), FormatAnnexB, t0, make([]byte, marker.PayloadSoftLimit+1))
	if !errors.Is(err, ErrPayloadAboveSoftLimit) || !marked || w.Sequence() != 1 {
		t.Fatalf("soft limit: marked=%v err=%v seq=%d", marked, err, w.Sequence())
	}

	ms, err := Markers(out, FormatAnnexB)
	if err != nil || len(ms) != 1 || len(ms[0].Payload) != marker.PayloadSoftLimit+1 {
		t.Fatalf("payload not carried: %v %v", ms, err)
	}
}

func TestMarkWithoutVCLIsAnError(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	if _, _, err := w.Mark(annexB(sps, pps), FormatAnnexB, t0, nil); !errors.Is(err, ErrNoVCL) {
		t.Fatalf("err = %v, want ErrNoVCL", err)
	}
}

func TestMarkReproducesSpecExample(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, _, err := w.Mark(annexB(idr), FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	nalus, _ := NALUnits(out, FormatAnnexB)
	if !bytes.Equal(nalus[0], exampleMarkerNAL(t)) {
		t.Fatalf("marker NAL unit\n got %x\nwant %x", nalus[0], exampleMarkerNAL(t))
	}
}

func TestMarkCaptureTimeSourceAndSequenceSeven(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{TimeSource: marker.TimeCapture})

	for range 7 {
		if _, _, err := w.Mark(annexB(nonIDR), FormatAnnexB, t0, nil); err != nil {
			t.Fatal(err)
		}
	}

	out, _, err := w.Mark(annexB(nonIDR), FormatAnnexB, t0, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	ms, _ := Markers(out, FormatAnnexB)
	want := unhex(t, "01 03 00 06 5b 4f 7b ed f4 00 00 00 00 07 9f 3c 1a 77 e2 b0 4d 51 00 05 68 65 6c 6c 6f")
	body, _ := ms[0].Encode()

	if !bytes.Equal(body, want) {
		t.Fatalf("body %x, want vector 006 %x", body, want)
	}
}

func TestMarkRoundsTimeToMicroseconds(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	out, _, err := w.Mark(annexB(idr), FormatAnnexB, t0.Add(1500*time.Nanosecond), nil)
	if err != nil {
		t.Fatal(err)
	}

	ms, _ := Markers(out, FormatAnnexB)
	if want := t0.Add(1 * time.Microsecond); !ms[0].OriginTime.Equal(want) {
		t.Fatalf("OriginTime %v, want %v", ms[0].OriginTime, want)
	}
}

func TestNewWriterRandomStreamID(t *testing.T) {
	t.Parallel()

	a, err := NewWriter(WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}

	b, err := NewWriter(WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if a.StreamID() == b.StreamID() || a.StreamID() == [marker.StreamIDSize]byte{} {
		t.Fatalf("stream ids %x and %x should be random and non-zero", a.StreamID(), b.StreamID())
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestStripMarkersRemovesOnlyMarkerSEI(t *testing.T) {
	t.Parallel()

	in := annexB(foreignSEINAL(t), exampleMarkerNAL(t), idr)

	out, err := StripMarkers(in, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := naluTypes(t, out, FormatAnnexB), []int{6, 5}; !equalInts(got, want) {
		t.Fatalf("NAL types %v, want %v", got, want)
	}

	ms, err := Markers(out, FormatAnnexB)
	if err != nil || len(ms) != 0 {
		t.Fatalf("markers left: %v %v", ms, err)
	}
}

func TestStripMarkersIsANoopWithoutMarkers(t *testing.T) {
	t.Parallel()

	in := annexB(sps, pps, idr)

	out, err := StripMarkers(in, FormatAnnexB)
	if err != nil || !bytes.Equal(out, in) {
		t.Fatalf("changed an unmarked unit: %v", err)
	}
}

func TestStripMarkersLengthPrefixed(t *testing.T) {
	t.Parallel()

	in := lengthPrefixed(exampleMarkerNAL(t), idr)

	out, err := StripMarkers(in, FormatLengthPrefixed)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := naluTypes(t, out, FormatLengthPrefixed), []int{5}; !equalInts(got, want) {
		t.Fatalf("NAL types %v, want %v", got, want)
	}
}

func TestStripThenMarkRoundTrip(t *testing.T) {
	t.Parallel()

	w := newTestWriter(t, WriterOptions{})

	marked, _, err := w.Mark(annexB(sps, pps, idr), FormatAnnexB, t0, nil)
	if err != nil {
		t.Fatal(err)
	}

	stripped, err := StripMarkers(marked, FormatAnnexB)
	if err != nil {
		t.Fatal(err)
	}

	w2 := newTestWriter(t, WriterOptions{})

	again, _, err := w2.Mark(stripped, FormatAnnexB, t0, nil)
	if err != nil || !bytes.Equal(again, marked) {
		t.Fatalf("round trip differs: %v", err)
	}
}
