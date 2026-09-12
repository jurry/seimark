// Package h264 works on H.264 access units: finding NAL units, finding seimark
// markers in them, and splitting Annex B byte streams into access units.
package h264

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Eyevinn/mp4ff/avc"
)

// Format is the framing of an access unit's NAL units: start codes or length prefixes.
type Format int

// FormatUnknown means DetectFormat could not tell Annex B from length-prefixed data.
// FormatAnnexB means NAL units are delimited by start codes.
// FormatLengthPrefixed means each NAL unit is preceded by a four-byte big-endian length.
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

// ErrUnknownFormat is returned when an access unit is in neither recognised framing.
var ErrUnknownFormat = errors.New("seimark: cannot tell Annex B from length-prefixed data")

// lengthPrefixSize is the width of the length prefix before each NAL unit in
// length-prefixed framing.
const lengthPrefixSize = 4

// minLengthPrefixedSize is a length prefix plus at least one NAL unit byte.
const minLengthPrefixedSize = lengthPrefixSize + 1

// DetectFormat guesses from the first bytes. A start code means Annex B; a
// plausible four-byte length means length-prefixed.
func DetectFormat(au []byte) Format {
	if hasStartCode(au) {
		return FormatAnnexB
	}

	if len(au) >= minLengthPrefixedSize {
		n := binary.BigEndian.Uint32(au[:lengthPrefixSize])
		if n >= 1 && int(n) <= len(au)-lengthPrefixSize {
			return FormatLengthPrefixed
		}
	}

	return FormatUnknown
}

// shortStartCodeSize and startCodeSize are the two Annex B start code widths:
// 00 00 01 and 00 00 00 01.
const (
	shortStartCodeSize = 3
	startCodeSize      = 4
)

func hasStartCode(b []byte) bool {
	return (len(b) >= shortStartCodeSize && b[0] == 0 && b[1] == 0 && b[2] == 1) ||
		(len(b) >= startCodeSize && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1)
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
		if len(au)-pos < lengthPrefixSize {
			return nil, fmt.Errorf("seimark: length-prefixed access unit: %d bytes left at offset %d, need a 4-byte length",
				len(au)-pos, pos)
		}

		length := int(binary.BigEndian.Uint32(au[pos : pos+lengthPrefixSize]))

		pos += lengthPrefixSize
		if length < 1 || length > len(au)-pos {
			return nil, fmt.Errorf("seimark: length-prefixed access unit: NAL unit length %d at offset %d, %d bytes left",
				length, pos-lengthPrefixSize, len(au)-pos)
		}

		nalus = append(nalus, au[pos:pos+length])
		pos += length
	}

	return nalus, nil
}
