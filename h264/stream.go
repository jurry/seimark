package h264

import (
	"bufio"
	"bytes"
	"io"
	"iter"

	"github.com/Eyevinn/mp4ff/avc"
)

// maxNALUnitSize bounds the scanner buffer; a single IDR picture never
// approaches it at the resolutions seimark is used with.
const maxNALUnitSize = 16 << 20

// initialScanBufferSize is the scanner's starting buffer; it grows to maxNALUnitSize as needed.
const initialScanBufferSize = 64 << 10

func startCode() []byte {
	return []byte{0, 0, 1}
}

// fourByteStartCode is what AccessUnits and carriesMarker prepend to each NAL
// unit; startCodeSize names its length.
func fourByteStartCode() []byte {
	return []byte{0, 0, 0, 1}
}

// AccessUnits splits an Annex B byte stream into access units. Each yielded
// unit uses four-byte start codes. A new unit begins, once the current one holds
// a VCL NAL unit, at a delimiter, SPS, PPS or SEI, or at a VCL NAL unit whose
// first_mb_in_slice is zero.
func AccessUnits(r io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		for au, err := range AccessUnitsAt(r) {
			if !yield(au.Bytes, err) {
				return
			}
		}
	}
}

// AccessUnit is one access unit with where it was found in the input.
type AccessUnit struct {
	// Bytes is the unit in Annex B form with four-byte start codes, which is
	// not the input's own framing: three-byte start codes are widened and
	// trailing zeros dropped, so Bytes is not Offset's slice of the input.
	Bytes []byte

	// Offset is the byte position in the input of the start code preceding the
	// unit's first NAL unit.
	Offset int
}

// AccessUnitsAt splits an Annex B byte stream into access units exactly as
// AccessUnits does, reporting with each unit where in the input it began. A
// caller that must map a unit back onto the input, such as a container reader
// attributing timestamps to byte ranges, needs the offset because a unit's own
// length is not the number of bytes it was cut from.
func AccessUnitsAt(r io.Reader) iter.Seq2[AccessUnit, error] {
	return func(yield func(AccessUnit, error) bool) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, initialScanBufferSize), maxNALUnitSize)

		var consumed, at int

		sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			advance, token, begin := splitNALUnits(data, atEOF)
			at = consumed + begin
			consumed += advance

			return advance, token, nil
		})

		var cur accessUnit

		for sc.Scan() {
			nal := sc.Bytes()
			if len(nal) == 0 {
				continue
			}

			if done, offset := cur.add(nal, at); done != nil &&
				!yield(AccessUnit{Bytes: done, Offset: offset}, nil) {
				return
			}
		}

		if err := sc.Err(); err != nil {
			yield(AccessUnit{}, err)

			return
		}

		if len(cur.bytes) > 0 {
			yield(AccessUnit{Bytes: cur.bytes, Offset: cur.offset}, nil)
		}
	}
}

// accessUnit accumulates the NAL units of one access unit.
type accessUnit struct {
	bytes  []byte
	offset int
	sawVCL bool
}

// add appends nal, found at offset at in the input, returning the finished
// access unit and its own offset when nal starts a new one.
func (a *accessUnit) add(nal []byte, at int) (done []byte, offset int) {
	t := avc.GetNaluType(nal[0])

	if a.sawVCL && startsAccessUnit(nal, t) {
		done, offset = a.bytes, a.offset
		*a = accessUnit{}
	}

	if len(a.bytes) == 0 {
		a.offset = at
	}

	a.bytes = append(a.bytes, fourByteStartCode()...)
	a.bytes = append(a.bytes, nal...)
	a.sawVCL = a.sawVCL || avc.IsVideoNaluType(t)

	return done, offset
}

// carriesMarker reports whether this one SEI NAL unit holds a seimark marker.
func carriesMarker(nal []byte) bool {
	one := make([]byte, 0, startCodeSize+len(nal))
	one = append(one, fourByteStartCode()...)
	one = append(one, nal...)
	ms, _ := Markers(one, FormatAnnexB)

	return len(ms) > 0
}

// naluHeaderSize is the one-byte NAL unit header; the slice header byte follows it.
const naluHeaderSize = 1

// firstBitMask isolates the top bit of the first slice header byte, where
// first_mb_in_slice's ue(v) encoding puts its leading 1 bit when the value is zero.
const firstBitMask = 0x80

func startsAccessUnit(nal []byte, t avc.NaluType) bool {
	switch t {
	case avc.NALU_AUD, avc.NALU_SPS, avc.NALU_PPS, avc.NALU_SEI:
		return true
	case avc.NALU_IDR, avc.NALU_NON_IDR:
		// first_mb_in_slice is the first ue(v) after the header; it is zero
		// exactly when the first bit is 1.
		return len(nal) > naluHeaderSize && nal[naluHeaderSize]&firstBitMask != 0
	case avc.NALU_EO_SEQ, avc.NALU_EO_STREAM, avc.NALU_FILL:
		return false
	}

	return false
}

// splitNALUnits is a bufio.SplitFunc yielding NAL units without start codes,
// with start reporting where in data the start code introducing the token
// begins. Trailing zero bytes belong to the next start code or are
// trailing_zero_8bits, so they are trimmed.
func splitNALUnits(data []byte, atEOF bool) (advance int, token []byte, start int) {
	sc := startCode()

	begin := bytes.Index(data, sc)
	if begin < 0 {
		if atEOF {
			return len(data), nil, len(data)
		}

		return 0, nil, 0
	}

	// A four-byte start code is the three-byte one with a leading zero.
	start = begin
	if begin > 0 && data[begin-1] == 0 {
		start = begin - 1
	}

	begin += len(sc)

	next := bytes.Index(data[begin:], sc)
	if next < 0 {
		if !atEOF {
			return 0, nil, 0
		}

		return len(data), trimZeros(data[begin:]), start
	}

	end := begin + next

	return end, trimZeros(data[begin:end]), start
}

func trimZeros(b []byte) []byte {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil
	}

	return b
}
