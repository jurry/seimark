package ts

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/asticode/go-astits"

	"github.com/jurry/seimark/container"
	"github.com/jurry/seimark/h264"
)

const (
	videoPID = 256
	audioPID = 257
	pmtPID   = 4096
)

// nal returns one Annex B NAL unit of the given type with filler payload.
func nal(nalType byte, payload ...byte) []byte {
	out := make([]byte, 0, 5+len(payload))
	out = append(out, 0, 0, 0, 1, nalType)

	return append(out, payload...)
}

// aud is an access unit delimiter, which starts an access unit in h264.AccessUnits.
func aud() []byte {
	return nal(0x09, 0xf0)
}

// slice is a VCL NAL unit whose first_mb_in_slice is zero.
func slice(nalType byte) []byte {
	return nal(nalType, 0x88, 0x84, 0x21, 0x3f)
}

func accessUnit(vcl byte) []byte {
	return append(aud(), slice(vcl)...)
}

type pesSpec struct {
	payload   []byte
	indicator uint8
	pts       int64
	dts       int64
}

// mux writes a transport stream carrying the given elementary streams and PES packets.
func mux(t *testing.T, streams []*astits.PMTElementaryStream, pid uint16, packets []pesSpec) []byte {
	t.Helper()

	var buf bytes.Buffer

	m := astits.NewMuxer(context.Background(), &buf)

	for _, es := range streams {
		if err := m.AddElementaryStream(*es); err != nil {
			t.Fatalf("AddElementaryStream(%d): %v", es.ElementaryPID, err)
		}
	}

	m.SetPCRPID(pid)

	for i, p := range packets {
		oh := &astits.PESOptionalHeader{
			MarkerBits:      0b10,
			PTSDTSIndicator: p.indicator,
		}

		switch p.indicator {
		case astits.PTSDTSIndicatorBothPresent:
			oh.PTS = &astits.ClockReference{Base: p.pts}
			oh.DTS = &astits.ClockReference{Base: p.dts}
		case astits.PTSDTSIndicatorOnlyPTS:
			oh.PTS = &astits.ClockReference{Base: p.pts}
		}

		d := &astits.MuxerData{
			PID: pid,
			PES: &astits.PESData{
				Data:   p.payload,
				Header: &astits.PESHeader{OptionalHeader: oh},
			},
		}

		if _, err := m.WriteData(d); err != nil {
			t.Fatalf("WriteData(%d): %v", i, err)
		}
	}

	return buf.Bytes()
}

func h264Stream() *astits.PMTElementaryStream {
	return &astits.PMTElementaryStream{ElementaryPID: videoPID, StreamType: astits.StreamTypeH264Video}
}

func aacStream() *astits.PMTElementaryStream {
	return &astits.PMTElementaryStream{ElementaryPID: audioPID, StreamType: astits.StreamTypeAACAudio}
}

// collect drains the sequence, returning the samples and the terminating error.
func collect(t *testing.T, b []byte) ([]container.Sample, error) {
	t.Helper()

	var (
		got []container.Sample
		err error
	)

	for s, e := range VideoSamples(bytes.NewReader(b)) {
		if e != nil {
			err = e

			break
		}

		got = append(got, s)
	}

	return got, err
}

// fixtureSamples reads the committed MPEG-TS stream vector.
func fixtureSamples(t *testing.T) []container.Sample {
	t.Helper()

	f, err := os.Open(filepath.Join("..", "vectors", "streams", "testsrc-marked.ts"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	var samples []container.Sample

	for s, err := range VideoSamples(f) {
		if err != nil {
			t.Fatalf("VideoSamples: %v", err)
		}

		samples = append(samples, s)
	}

	return samples
}

func TestFixtureSamples(t *testing.T) {
	t.Parallel()

	samples := fixtureSamples(t)

	if len(samples) != 20 {
		t.Fatalf("got %d samples, want 20", len(samples))
	}

	if !samples[0].Sync {
		t.Error("sample 0: Sync = false, want true")
	}

	for i, s := range samples {
		if s.Index != i {
			t.Errorf("sample %d: Index = %d", i, s.Index)
		}

		if s.Timescale != Timescale {
			t.Errorf("sample %d: Timescale = %d, want %d", i, s.Timescale, Timescale)
		}

		if s.Framing != container.FramingAnnexB {
			t.Errorf("sample %d: Framing = %v, want annexb", i, s.Framing)
		}

		if !bytes.HasPrefix(s.Data, []byte{0, 0, 0, 1}) {
			t.Errorf("sample %d: Data does not start with a four-byte start code", i)
		}

		if i > 0 && s.DTS < samples[i-1].DTS {
			t.Errorf("sample %d: DTS = %d, below the previous %d", i, s.DTS, samples[i-1].DTS)
		}
	}
}

func TestMarkersSurviveTheDemux(t *testing.T) {
	t.Parallel()

	samples := fixtureSamples(t)

	if len(samples) != 20 {
		t.Fatalf("got %d samples, want 20", len(samples))
	}

	for i, s := range samples {
		ms, err := h264.Markers(s.Data, s.Framing.H264())
		if err != nil {
			t.Fatalf("sample %d: Markers: %v", i, err)
		}

		if len(ms) != 1 {
			t.Fatalf("sample %d: %d markers, want 1", i, len(ms))
		}

		if ms[0].Sequence != uint32(i) {
			t.Errorf("sample %d: Sequence = %d", i, ms[0].Sequence)
		}
	}
}

func TestSyncsFromFirstIDR(t *testing.T) {
	t.Parallel()

	const idr = 0x05

	b := mux(t, []*astits.PMTElementaryStream{h264Stream()}, videoPID, []pesSpec{
		{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 9000},
		{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 18000, dts: 18000},
		{payload: accessUnit(idr), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 27000},
	})

	got, err := collect(t, b)
	if err != nil {
		t.Fatalf("VideoSamples: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d samples, want 1", len(got))
	}

	if got[0].Index != 0 {
		t.Errorf("Index = %d, want 0", got[0].Index)
	}

	if !got[0].Sync {
		t.Error("Sync = false, want true")
	}

	units, err := h264.NALUnits(got[0].Data, h264.FormatAnnexB)
	if err != nil {
		t.Fatalf("NALUnits: %v", err)
	}

	var sawIDR bool

	for _, u := range units {
		if u[0]&0x1f == idr {
			sawIDR = true
		}
	}

	if !sawIDR {
		t.Error("the yielded unit does not contain the IDR NAL unit")
	}
}

func TestPESTimestampsApplyToTheAccessUnitStartingInIt(t *testing.T) {
	t.Parallel()

	b := mux(t, []*astits.PMTElementaryStream{h264Stream()}, videoPID, []pesSpec{
		{payload: accessUnit(0x05), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
		{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 15000},
	})

	got, err := collect(t, b)
	if err != nil {
		t.Fatalf("VideoSamples: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d samples, want 2", len(got))
	}

	for i, want := range []struct {
		dts uint64
		pts int64
	}{{6000, 9000}, {15000, 27000}} {
		if got[i].DTS != want.dts || got[i].PTS != want.pts {
			t.Errorf("sample %d: DTS = %d, PTS = %d; want %d, %d", i, got[i].DTS, got[i].PTS, want.dts, want.pts)
		}

		if got[i].Timescale != Timescale {
			t.Errorf("sample %d: Timescale = %d, want %d", i, got[i].Timescale, Timescale)
		}

		if got[i].Framing != container.FramingAnnexB {
			t.Errorf("sample %d: Framing = %v, want annexb", i, got[i].Framing)
		}
	}
}

func TestPESWithoutDTSTakesDTSFromPTS(t *testing.T) {
	t.Parallel()

	b := mux(t, []*astits.PMTElementaryStream{h264Stream()}, videoPID, []pesSpec{
		{payload: accessUnit(0x05), indicator: astits.PTSDTSIndicatorOnlyPTS, pts: 126000},
	})

	got, err := collect(t, b)
	if err != nil {
		t.Fatalf("VideoSamples: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d samples, want 1", len(got))
	}

	if got[0].PTS != 126000 || got[0].DTS != uint64(got[0].PTS) {
		t.Errorf("DTS = %d, PTS = %d; want both 126000", got[0].DTS, got[0].PTS)
	}
}

func TestNoH264StreamIsNoVideoTrack(t *testing.T) {
	t.Parallel()

	b := mux(t, []*astits.PMTElementaryStream{aacStream()}, audioPID, []pesSpec{
		{payload: []byte{1, 2, 3, 4}, indicator: astits.PTSDTSIndicatorOnlyPTS, pts: 9000},
	})

	got, err := collect(t, b)
	if !errors.Is(err, ErrNoVideoTrack) {
		t.Fatalf("err = %v, want ErrNoVideoTrack", err)
	}

	if len(got) != 0 {
		t.Errorf("got %d samples, want 0", len(got))
	}

	if !strings.Contains(err.Error(), "15") {
		t.Errorf("error %q does not name the stream type 15 that was seen", err)
	}
}

func TestGarbageIsMalformed(t *testing.T) {
	t.Parallel()

	_, err := collect(t, bytes.Repeat([]byte("not a transport stream"), 100))
	if !errors.Is(err, ErrMalformedTS) {
		t.Fatalf("err = %v, want ErrMalformedTS", err)
	}
}

func TestFirstH264StreamIsChosen(t *testing.T) {
	t.Parallel()

	b := mux(t, []*astits.PMTElementaryStream{aacStream(), h264Stream()}, videoPID, []pesSpec{
		{payload: accessUnit(0x05), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 9000},
	})

	got, err := collect(t, b)
	if err != nil {
		t.Fatalf("VideoSamples: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d samples, want 1", len(got))
	}
}

// dtsOf returns the DTS of every sample, which is what timestamp attribution
// gets wrong when the payload offset of an access unit drifts.
func dtsOf(samples []container.Sample) []uint64 {
	out := make([]uint64, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.DTS)
	}

	return out
}

func TestTimestampsSurvivePaddingAndShortStartCodes(t *testing.T) {
	t.Parallel()

	// threeByte re-frames an access unit with three-byte start codes.
	threeByte := func(au []byte) []byte {
		return bytes.ReplaceAll(au, []byte{0, 0, 0, 1}, []byte{0, 0, 1})
	}

	for _, tc := range []struct {
		name    string
		packets []pesSpec
		want    []uint64
	}{
		{
			name: "bare start code between NAL units",
			packets: []pesSpec{
				{payload: append(aud(), append([]byte{0, 0, 0, 1}, slice(0x05)...)...), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
				{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 15000},
			},
			want: []uint64{6000, 15000},
		},
		{
			// A three-byte start code directly after a four-byte one, which
			// widens by a byte and so shifts every later offset.
			name: "back-to-back start codes",
			packets: []pesSpec{
				{payload: append(aud(), append([]byte{0, 0, 1, 0x06, 0x00, 0x80}, slice(0x05)...)...), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
				{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 15000},
				{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 36000, dts: 24000},
			},
			want: []uint64{6000, 15000, 24000},
		},
		{
			name: "three-byte start codes",
			packets: []pesSpec{
				{payload: threeByte(accessUnit(0x05)), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
				{payload: threeByte(accessUnit(0x01)), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 15000},
				{payload: threeByte(accessUnit(0x01)), indicator: astits.PTSDTSIndicatorBothPresent, pts: 36000, dts: 24000},
			},
			want: []uint64{6000, 15000, 24000},
		},
		{
			name: "access unit spanning two PES packets",
			packets: []pesSpec{
				{payload: aud(), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
				{payload: slice(0x05), indicator: astits.PTSDTSIndicatorBothPresent, pts: 27000, dts: 15000},
				{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 36000, dts: 24000},
			},
			// The split unit takes the timestamps of the PES its first byte arrived in.
			want: []uint64{6000, 24000},
		},
		{
			name: "two access units in one PES",
			packets: []pesSpec{
				{payload: append(accessUnit(0x05), accessUnit(0x01)...), indicator: astits.PTSDTSIndicatorBothPresent, pts: 9000, dts: 6000},
				{payload: accessUnit(0x01), indicator: astits.PTSDTSIndicatorBothPresent, pts: 36000, dts: 24000},
			},
			want: []uint64{6000, 6000, 24000},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := mux(t, []*astits.PMTElementaryStream{h264Stream()}, videoPID, tc.packets)

			got, err := collect(t, b)
			if err != nil {
				t.Fatalf("VideoSamples: %v", err)
			}

			if dts := dtsOf(got); !slices.Equal(dts, tc.want) {
				t.Errorf("DTS = %v, want %v", dts, tc.want)
			}
		})
	}
}

func TestTruncatedStreamIsMalformed(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile(filepath.Join("..", "vectors", "streams", "testsrc-marked.ts"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := collect(t, b[:len(b)-100])
	if !errors.Is(err, ErrMalformedTS) {
		t.Fatalf("err = %v, want ErrMalformedTS", err)
	}

	if len(got) == 0 {
		t.Error("no samples were yielded before the error")
	}

	if len(got) > 20 {
		t.Errorf("got %d samples, more than the whole stream's 20", len(got))
	}
}

// TestContainsIDRReturnsItsSplitError pins the contract that a unit which does
// not split is an error rather than a silently non-sync unit. Annex B splitting
// cannot currently fail, so the error is checked on the format that can.
func TestContainsIDRReturnsItsSplitError(t *testing.T) {
	t.Parallel()

	if _, err := h264.NALUnits([]byte{0, 0, 0, 0}, h264.FormatLengthPrefixed); err == nil {
		t.Fatal("a zero length prefix should not split")
	}

	sync, err := containsIDR(accessUnit(0x05))
	if err != nil || !sync {
		t.Fatalf("containsIDR(IDR unit) = %v, %v; want true, nil", sync, err)
	}
}
