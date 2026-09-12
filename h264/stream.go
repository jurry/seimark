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
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, initialScanBufferSize), maxNALUnitSize)
		sc.Split(splitNALUnits)

		var cur accessUnit

		for sc.Scan() {
			nal := sc.Bytes()
			if len(nal) == 0 {
				continue
			}

			if done := cur.add(nal); done != nil && !yield(done, nil) {
				return
			}
		}

		if err := sc.Err(); err != nil {
			yield(nil, err)

			return
		}

		if len(cur.bytes) > 0 {
			yield(cur.bytes, nil)
		}
	}
}

// accessUnit accumulates the NAL units of one access unit.
type accessUnit struct {
	bytes  []byte
	sawVCL bool
}

// add appends nal, returning the finished access unit when nal starts a new one.
func (a *accessUnit) add(nal []byte) []byte {
	t := avc.GetNaluType(nal[0])

	var done []byte
	if a.sawVCL && startsAccessUnit(nal, t) {
		done = a.bytes
		*a = accessUnit{}
	}

	a.bytes = append(a.bytes, fourByteStartCode()...)
	a.bytes = append(a.bytes, nal...)
	a.sawVCL = a.sawVCL || avc.IsVideoNaluType(t)

	return done
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

// splitNALUnits is a bufio.SplitFunc yielding NAL units without start codes.
// Trailing zero bytes belong to the next start code or are trailing_zero_8bits,
// so they are trimmed.
func splitNALUnits(data []byte, atEOF bool) (advance int, token []byte, err error) {
	sc := startCode()

	begin := bytes.Index(data, sc)
	if begin < 0 {
		if atEOF {
			return len(data), nil, nil
		}

		return 0, nil, nil
	}

	begin += len(sc)

	next := bytes.Index(data[begin:], sc)
	if next < 0 {
		if !atEOF {
			return 0, nil, nil
		}

		return len(data), trimZeros(data[begin:]), nil
	}

	end := begin + next

	return end, trimZeros(data[begin:end]), nil
}

func trimZeros(b []byte) []byte {
	b = bytes.TrimRight(b, "\x00")
	if len(b) == 0 {
		return nil
	}

	return b
}
