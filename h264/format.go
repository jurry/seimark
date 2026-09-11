// Package h264 works on H.264 access units: finding NAL units, finding seimark
// markers in them, and splitting Annex B byte streams into access units.
package h264

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Eyevinn/mp4ff/avc"
)

type Format int

const (
	FormatUnknown Format = iota
	FormatAnnexB
	FormatLengthPrefixed
)

func (f Format) String() string {
	switch f {
	case FormatAnnexB:
		return "annexb"
	case FormatLengthPrefixed:
		return "length-prefixed"
	}
	return "unknown"
}

var ErrUnknownFormat = errors.New("seimark: cannot tell Annex B from length-prefixed data")

// DetectFormat guesses from the first bytes. A start code means Annex B; a
// plausible four-byte length means length-prefixed.
func DetectFormat(au []byte) Format {
	if hasStartCode(au) {
		return FormatAnnexB
	}
	if len(au) >= 5 {
		n := binary.BigEndian.Uint32(au[:4])
		if n >= 1 && int(n) <= len(au)-4 {
			return FormatLengthPrefixed
		}
	}
	return FormatUnknown
}

func hasStartCode(b []byte) bool {
	return (len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1) ||
		(len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1)
}

// NALUnits splits an access unit into NAL units without start codes or length
// prefixes. The returned slices alias au.
func NALUnits(au []byte, f Format) ([][]byte, error) {
	switch f {
	case FormatAnnexB:
		return avc.ExtractNalusFromByteStream(au), nil
	case FormatLengthPrefixed:
		nalus, err := avc.GetNalusFromSample(au)
		if err != nil {
			return nil, fmt.Errorf("seimark: length-prefixed access unit: %w", err)
		}
		return nalus, nil
	}
	return nil, ErrUnknownFormat
}
