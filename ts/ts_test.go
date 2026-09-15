package ts

import (
	"bytes"
	"context"
	"errors"
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

func TestFixtureSamples(t *testing.T) {
	t.Parallel()
	t.Skip("vectors/streams/testsrc-marked.ts is generated in task 7")
}

func TestMarkersSurviveTheDemux(t *testing.T) {
	t.Parallel()
	t.Skip("vectors/streams/testsrc-marked.ts is generated in task 7")
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
