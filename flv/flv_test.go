package flv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jurry/seimark/container"
)

// header is the nine-byte FLV header with a video-only flags byte, followed by
// the first previous-tag size.
func header() []byte {
	return []byte{'F', 'L', 'V', 1, 0x01, 0, 0, 0, 9, 0, 0, 0, 0}
}

// tag builds one FLV tag with its trailing previous-tag size.
func tag(tagType byte, ts uint32, data []byte) []byte {
	b := make([]byte, 0, tagHeaderSize+len(data)+prevTagSize)
	b = append(b,
		tagType,
		byte(len(data)>>16), byte(len(data)>>8), byte(len(data)),
		byte(ts>>16), byte(ts>>8), byte(ts), byte(ts>>24),
		0, 0, 0,
	)
	b = append(b, data...)

	return binary.BigEndian.AppendUint32(b, uint32(len(data))+tagHeaderSize)
}

func videoTag(ts uint32, frameType, packetType byte, cts int32, payload []byte) []byte {
	data := make([]byte, 0, avcHeaderSize+len(payload))
	data = append(data, frameType<<4|codecIDAVC, packetType, byte(cts>>16), byte(cts>>8), byte(cts))
	data = append(data, payload...)

	return tag(tagTypeVideo, ts, data)
}

func audioTag(ts uint32) []byte {
	return tag(tagTypeAudio, ts, []byte{0xAF, 0x01, 0x21, 0x10})
}

func scriptTag() []byte {
	return tag(tagTypeScript, 0, []byte{0x02, 0x00, 0x0A, 'o', 'n', 'M', 'e', 't', 'a', 'D', 'a', 't', 'a'})
}

func seqHeader(sps, pps []byte, lengthSize byte) []byte {
	rec := make([]byte, 0, 8+len(sps)+3+len(pps))
	rec = append(rec,
		1, 0x42, 0x00, 0x1E, 0xFC|(lengthSize-1), 0xE0|1,
		byte(len(sps)>>8), byte(len(sps)),
	)
	rec = append(rec, sps...)
	rec = append(rec, 1, byte(len(pps)>>8), byte(len(pps)))
	rec = append(rec, pps...)

	return videoTag(0, frameTypeKeyframe, packetTypeSequenceHeader, 0, rec)
}

// nalPayload is one length-prefixed NAL unit, the shape a sample tag carries.
func nalPayload(nal []byte) []byte {
	p := make([]byte, 0, requiredLengthSize+len(nal))
	p = binary.BigEndian.AppendUint32(p, uint32(len(nal)))

	return append(p, nal...)
}

func collect(t *testing.T, in []byte) ([]container.Sample, error) {
	t.Helper()

	var out []container.Sample

	for s, err := range VideoSamples(bytes.NewReader(in)) {
		if err != nil {
			return out, err
		}

		out = append(out, s)
	}

	return out, nil
}

func mustCollect(t *testing.T, in []byte) []container.Sample {
	t.Helper()

	samples, err := collect(t, in)
	if err != nil {
		t.Fatalf("VideoSamples: %v", err)
	}

	return samples
}

func TestFixtureSamples(t *testing.T) {
	t.Parallel()
	t.Skip("vectors/streams/testsrc-marked.flv is generated in task 7")

	f, err := os.Open(filepath.Join("..", "vectors", "streams", "testsrc-marked.flv"))
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

	if len(samples) != 20 {
		t.Fatalf("got %d samples, want 20", len(samples))
	}

	for i, s := range samples {
		if s.Index != i {
			t.Errorf("sample %d: Index = %d", i, s.Index)
		}

		if s.Timescale != Timescale {
			t.Errorf("sample %d: Timescale = %d, want %d", i, s.Timescale, Timescale)
		}

		if s.Framing != container.FramingLengthPrefixed {
			t.Errorf("sample %d: Framing = %v", i, s.Framing)
		}

		if i > 0 && s.DTS <= samples[i-1].DTS {
			t.Errorf("sample %d: DTS %d not after %d", i, s.DTS, samples[i-1].DTS)
		}
	}

	if !samples[0].Sync {
		t.Error("sample 0: Sync = false, want true")
	}
}

func TestSequenceHeaderExposesParameterSets(t *testing.T) {
	t.Parallel()

	sps := []byte{0x67, 0x42, 0xC0, 0x1E}
	pps := []byte{0x68, 0xCE, 0x3C, 0x80}

	in := header()
	in = append(in, seqHeader(sps, pps, 4)...)
	in = append(in, videoTag(100, 1, 1, 0, nalPayload([]byte{0x65, 0x88}))...)

	s, err := NewStream(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}

	var samples []container.Sample

	for sample, err := range s.Samples() {
		if err != nil {
			t.Fatalf("Samples: %v", err)
		}

		samples = append(samples, sample)
	}

	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}

	if !bytes.Equal(s.SPS(), sps) {
		t.Errorf("SPS() = % x, want % x", s.SPS(), sps)
	}

	if !bytes.Equal(s.PPS(), pps) {
		t.Errorf("PPS() = % x, want % x", s.PPS(), pps)
	}
}

func TestCompositionOffset(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)
	in = append(in, videoTag(100, 1, 1, 33, nalPayload([]byte{0x65}))...)
	in = append(in, videoTag(100, 2, 1, -1, nalPayload([]byte{0x41}))...)

	samples := mustCollect(t, in)
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2", len(samples))
	}

	if samples[0].DTS != 100 || samples[0].PTS != 133 {
		t.Errorf("sample 0: DTS = %d, PTS = %d, want 100 and 133", samples[0].DTS, samples[0].PTS)
	}

	if samples[1].DTS != 100 || samples[1].PTS != 99 {
		t.Errorf("sample 1: DTS = %d, PTS = %d, want 100 and 99", samples[1].DTS, samples[1].PTS)
	}
}

func TestTimestampExtension(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)
	in = append(in, videoTag(0x01000001, 1, 1, 0, nalPayload([]byte{0x65}))...)

	samples := mustCollect(t, in)
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}

	if samples[0].DTS != 16777217 {
		t.Errorf("DTS = %d, want 16777217", samples[0].DTS)
	}
}

func TestKeyframeFlag(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)
	in = append(in, videoTag(0, 1, 1, 0, nalPayload([]byte{0x65}))...)
	in = append(in, videoTag(100, 2, 1, 0, nalPayload([]byte{0x41}))...)

	samples := mustCollect(t, in)
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2", len(samples))
	}

	if !samples[0].Sync {
		t.Error("sample 0: Sync = false, want true")
	}

	if samples[1].Sync {
		t.Error("sample 1: Sync = true, want false")
	}
}

func TestSkipsNonVideoTags(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, scriptTag()...)
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)
	in = append(in, videoTag(0, 1, 1, 0, nalPayload([]byte{0x65}))...)
	in = append(in, audioTag(50)...)
	in = append(in, scriptTag()...)
	in = append(in, videoTag(100, 2, 1, 0, nalPayload([]byte{0x41}))...)

	samples := mustCollect(t, in)
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2", len(samples))
	}

	if samples[0].Index != 0 || samples[1].Index != 1 {
		t.Errorf("Index = %d and %d, want 0 and 1", samples[0].Index, samples[1].Index)
	}
}

func TestEndOfSequenceTagYieldsNoSample(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)
	in = append(in, videoTag(0, 1, 1, 0, nalPayload([]byte{0x65}))...)
	in = append(in, videoTag(100, 1, 2, 0, nil)...)

	samples := mustCollect(t, in)
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}
}

func TestNotFLV(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"wrong signature", append([]byte("XXX"), make([]byte, 16)...)},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := collect(t, tc.in); !errors.Is(err, ErrNotFLV) {
				t.Errorf("err = %v, want ErrNotFLV", err)
			}
		})
	}
}

func TestEnhancedRTMPIsNoVideoTrack(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, tag(9, 0, append([]byte{0x91, 'a', 'v', '0', '1'}, 0x00))...)

	_, err := collect(t, in)
	if !errors.Is(err, ErrNoVideoTrack) {
		t.Fatalf("err = %v, want ErrNoVideoTrack", err)
	}

	if !strings.Contains(err.Error(), "av01") {
		t.Errorf("err = %v, want it to name av01", err)
	}
}

func TestLengthSizeOtherThanFourIsMalformed(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 2)...)

	if _, err := collect(t, in); !errors.Is(err, ErrMalformedFLV) {
		t.Errorf("err = %v, want ErrMalformedFLV", err)
	}
}

func TestTruncatedTagIsMalformed(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, seqHeader([]byte{0x67}, []byte{0x68}, 4)...)

	full := videoTag(0, 1, 1, 0, nalPayload([]byte{0x65, 0x88, 0x99}))
	in = append(in, full[:len(full)-6]...)

	_, err := collect(t, in)
	if !errors.Is(err, ErrMalformedFLV) {
		t.Fatalf("err = %v, want ErrMalformedFLV", err)
	}

	if !strings.Contains(err.Error(), "offset") {
		t.Errorf("err = %v, want it to name an offset", err)
	}
}

func TestNALTagBeforeSequenceHeaderIsMalformed(t *testing.T) {
	t.Parallel()

	in := header()
	in = append(in, videoTag(0, 1, 1, 0, nalPayload([]byte{0x65}))...)

	if _, err := collect(t, in); !errors.Is(err, ErrMalformedFLV) {
		t.Errorf("err = %v, want ErrMalformedFLV", err)
	}
}
