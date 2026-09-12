package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jurry/seimark/h264"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "vectors", "streams", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func collect(t *testing.T, name string) []Sample {
	t.Helper()
	var out []Sample
	for s, err := range VideoSamples(openFixture(t, name)) {
		if err != nil {
			t.Fatalf("VideoSamples(%s): %v", name, err)
		}
		out = append(out, s)
	}
	return out
}

func checkFixture(t *testing.T, samples []Sample) {
	t.Helper()
	if len(samples) != 20 {
		t.Fatalf("got %d samples, want 20", len(samples))
	}
	for i, s := range samples {
		if s.Index != i {
			t.Errorf("sample %d: Index = %d", i, s.Index)
		}
		if s.Timescale == 0 {
			t.Errorf("sample %d: Timescale = 0", i)
		}
		if i > 0 && s.DTS <= samples[i-1].DTS {
			t.Errorf("sample %d: DTS %d not after %d", i, s.DTS, samples[i-1].DTS)
		}
		if s.PTS < int64(s.DTS) {
			t.Errorf("sample %d: PTS %d before DTS %d", i, s.PTS, s.DTS)
		}
		wantSync := i%10 == 0
		if s.Sync != wantSync {
			t.Errorf("sample %d: Sync = %v, want %v", i, s.Sync, wantSync)
		}
		ms, err := h264.Markers(s.Data, h264.FormatLengthPrefixed)
		if err != nil || len(ms) != 1 || ms[0].Sequence != uint32(i) {
			t.Errorf("sample %d: markers %v, err %v", i, ms, err)
		}
	}
	// 100 ms apart at the track timescale.
	step := samples[1].PTS - samples[0].PTS
	if step != int64(samples[0].Timescale)/10 {
		t.Errorf("PTS step %d, want %d", step, samples[0].Timescale/10)
	}
}

func TestVideoSamplesProgressive(t *testing.T) {
	t.Parallel()
	checkFixture(t, collect(t, "testsrc-marked.mp4"))
}

func TestVideoSamplesFragmented(t *testing.T) {
	t.Parallel()
	checkFixture(t, collect(t, "testsrc-marked-frag.mp4"))
}

func TestVideoSamplesNoVideoTrack(t *testing.T) {
	t.Parallel()
	// An MP4 with only an ftyp box: no moov, no video.
	ftyp := []byte{0, 0, 0, 0x14, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm'}
	var sawErr error
	for _, err := range VideoSamples(bytes.NewReader(ftyp)) {
		sawErr = err
	}
	if !errors.Is(sawErr, ErrNoVideoTrack) {
		t.Fatalf("err = %v, want ErrNoVideoTrack", sawErr)
	}
}

func TestVideoSamplesGarbage(t *testing.T) {
	t.Parallel()
	var sawErr error
	for _, err := range VideoSamples(bytes.NewReader([]byte("not an mp4 file at all"))) {
		sawErr = err
	}
	if sawErr == nil {
		t.Fatal("garbage accepted")
	}
}

// fixtureBytes returns the progressive fixture's bytes for a test to corrupt.
func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "vectors", "streams", "testsrc-marked.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// boxPayload returns the offset of the first payload byte of the named box.
func boxPayload(t *testing.T, data []byte, typ string) int {
	t.Helper()
	i := bytes.Index(data, []byte(typ))
	if i < 4 {
		t.Fatalf("box %s not found", typ)
	}
	return i + 4
}

func collectErr(t *testing.T, data []byte) error {
	t.Helper()
	var sawErr error
	for _, err := range VideoSamples(bytes.NewReader(data)) {
		if err != nil {
			sawErr = err
		}
	}
	return sawErr
}

func TestVideoSamplesSttsShorterThanStsz(t *testing.T) {
	t.Parallel()
	data := fixtureBytes(t)
	// stts: version+flags, entry count, then (sample count, delta) pairs.
	p := boxPayload(t, data, "stts")
	binary.BigEndian.PutUint32(data[p+8:p+12], 5) // 5 samples covered, stsz says 20
	err := collectErr(t, data)
	if !errors.Is(err, ErrMalformedFile) {
		t.Fatalf("err = %v, want ErrMalformedFile", err)
	}
}

func TestVideoSamplesEmptyStco(t *testing.T) {
	t.Parallel()
	data := fixtureBytes(t)
	p := boxPayload(t, data, "stco")
	// Drop the single offset: entry count to 0, box and its parents four bytes
	// shorter, so mp4ff decodes a well-formed but empty table.
	binary.BigEndian.PutUint32(data[p+4:p+8], 0)
	data = append(data[:p+8:p+8], data[p+12:]...)
	shrinkBox(t, data, "stco", 4)
	for _, parent := range []string{"stbl", "minf", "mdia", "trak", "moov"} {
		shrinkBox(t, data, parent, 4)
	}
	err := collectErr(t, data)
	if !errors.Is(err, ErrMalformedFile) {
		t.Fatalf("err = %v, want ErrMalformedFile", err)
	}
}

// shrinkBox subtracts n from the size field of the named box.
func shrinkBox(t *testing.T, data []byte, typ string, n uint32) {
	t.Helper()
	p := boxPayload(t, data, typ) - 8
	binary.BigEndian.PutUint32(data[p:p+4], binary.BigEndian.Uint32(data[p:p+4])-n)
}
