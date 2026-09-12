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
	case FormatUnknown:
		return "unknown"
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
		return lengthPrefixedNALUnits(au)
	case FormatUnknown:
		return nil, ErrUnknownFormat
	}
	return nil, ErrUnknownFormat
}

// lengthPrefixedNALUnits walks four-byte big-endian length prefixes. A length
// that is zero or runs past the end of au is a corrupt sample, not a panic.
func lengthPrefixedNALUnits(au []byte) ([][]byte, error) {
	var nalus [][]byte
	for pos := 0; pos < len(au); {
		if len(au)-pos < 4 {
			return nil, fmt.Errorf("seimark: length-prefixed access unit: %d bytes left at offset %d, need a 4-byte length",
				len(au)-pos, pos)
		}
		length := int(binary.BigEndian.Uint32(au[pos : pos+4]))
		pos += 4
		if length < 1 || length > len(au)-pos {
			return nil, fmt.Errorf("seimark: length-prefixed access unit: NAL unit length %d at offset %d, %d bytes left",
				length, pos-4, len(au)-pos)
		}
		nalus = append(nalus, au[pos:pos+length])
		pos += length
	}
	return nalus, nil
}
