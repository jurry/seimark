// Package container holds the sample the container readers yield.
package container

import "github.com/jurry/seimark/h264"

// Framing says how a sample's Data carries its NAL units.
type Framing int

// FramingLengthPrefixed means four-byte big-endian lengths, as MP4 and FLV store them.
// FramingAnnexB means start codes, as MPEG-TS carries them.
const (
	FramingLengthPrefixed Framing = iota
	FramingAnnexB
)

// H264 is the h264.Format the marker and NAL unit calls take for this framing.
func (f Framing) H264() h264.Format {
	switch f {
	case FramingLengthPrefixed:
		return h264.FormatLengthPrefixed
	case FramingAnnexB:
		return h264.FormatAnnexB
	}

	return h264.FormatLengthPrefixed
}

// String names the framing as h264.Format spells it: "length-prefixed" or "annexb".
func (f Framing) String() string {
	return f.H264().String()
}

// Sample is one video sample with its timing in Timescale units.
type Sample struct {
	// Index counts from 0 at the first sample the reader yields, which for a
	// transport stream is the first IDR and not the first sample in the file.
	Index int

	// DTS is the decode time in Timescale units, as the container states it;
	// a 33-bit MPEG-TS clock is never unwrapped.
	DTS uint64

	// PTS is the presentation time in Timescale units. It is signed because an
	// MP4 composition offset may put it before the decode time.
	PTS int64

	// Timescale is the number of those units in a second, so seconds are
	// PTS/Timescale. It differs per container, not per sample.
	Timescale uint32

	// Sync reports a sample that can be decoded without an earlier one, which
	// each reader derives its own way: a container flag, or an IDR NAL unit.
	Sync bool

	// Framing says how Data carries its NAL units, and is what Markers and
	// NALUnits need; a consumer assuming one framing finds no markers in the other.
	Framing Framing

	// Data is the sample's NAL units in Framing's form, without any parameter
	// sets the container keeps outside the samples.
	Data []byte
}
