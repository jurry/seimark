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

var startCode = []byte{0, 0, 1}

// AccessUnits splits an Annex B byte stream into access units. Each yielded
// unit uses four-byte start codes. A new unit begins, once the current one holds
// a VCL NAL unit, at a delimiter, SPS, PPS or SEI, or at a VCL NAL unit whose
// first_mb_in_slice is zero.
func AccessUnits(r io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), maxNALUnitSize)
		sc.Split(splitNALUnits)
		var au []byte
		sawVCL := false
		for sc.Scan() {
			nal := sc.Bytes()
			if len(nal) == 0 {
				continue
			}
			t := avc.GetNaluType(nal[0])
			if sawVCL && startsAccessUnit(nal, t) {
				if !yield(au, nil) {
					return
				}
				au = nil
				sawVCL = false
			}
			au = append(au, 0, 0, 0, 1)
			au = append(au, nal...)
			if avc.IsVideoNaluType(t) {
				sawVCL = true
			}
		}
		if err := sc.Err(); err != nil {
			yield(nil, err)
			return
		}
		if len(au) > 0 {
			yield(au, nil)
		}
	}
}

func startsAccessUnit(nal []byte, t avc.NaluType) bool {
	switch t {
	case avc.NALU_AUD, avc.NALU_SPS, avc.NALU_PPS, avc.NALU_SEI:
		return true
	case avc.NALU_IDR, avc.NALU_NON_IDR:
		// first_mb_in_slice is the first ue(v) after the header; it is zero
		// exactly when the first bit is 1.
		return len(nal) > 1 && nal[1]&0x80 != 0
	}
	return false
}

// splitNALUnits is a bufio.SplitFunc yielding NAL units without start codes.
// Trailing zero bytes belong to the next start code or are trailing_zero_8bits,
// so they are trimmed.
func splitNALUnits(data []byte, atEOF bool) (int, []byte, error) {
	begin := bytes.Index(data, startCode)
	if begin < 0 {
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	}
	begin += len(startCode)
	next := bytes.Index(data[begin:], startCode)
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
