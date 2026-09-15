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

func (f Framing) String() string {
	return f.H264().String()
}

// Sample is one video sample with its timing in Timescale units.
type Sample struct {
	Index     int
	DTS       uint64
	PTS       int64
	Timescale uint32
	Sync      bool
	Framing   Framing
	Data      []byte
}
