package h264

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Eyevinn/mp4ff/avc"

	"github.com/jurry/seimark/marker"
)

// ErrAlreadyMarked reports an access unit that already carries a seimark
// marker; the unit is left alone and the sequence does not advance.
var ErrAlreadyMarked = errors.New("seimark: access unit already carries a marker")

// ErrPayloadAboveSoftLimit reports a payload past marker.PayloadSoftLimit. It is
// advisory: the unit was marked and is returned with it.
var ErrPayloadAboveSoftLimit = errors.New("seimark: payload above the soft limit")

// ErrNoVCL reports an access unit with no VCL NAL unit, which has no place for
// the marker.
var ErrNoVCL = errors.New("seimark: access unit has no VCL NAL unit")

// WriterOptions configures a Writer. The zero value means a random stream id,
// every access unit marked, and time of sending.
type WriterOptions struct {
	StreamID      [marker.StreamIDSize]byte
	KeyframesOnly bool
	TimeSource    marker.TimeSource
}

// Writer stamps access units of one stream, keeping the stream id and the
// sequence number between calls. It is not safe for concurrent use.
type Writer struct {
	opts WriterOptions
	seq  uint32
}

// NewWriter returns a Writer for one stream, drawing a random stream id when the
// options leave it zero.
func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.StreamID == [marker.StreamIDSize]byte{} {
		if _, err := rand.Read(opts.StreamID[:]); err != nil {
			return nil, fmt.Errorf("seimark: random stream id: %w", err)
		}
	}

	return &Writer{opts: opts}, nil
}

// StreamID returns the stream id every marker this Writer writes carries.
func (w *Writer) StreamID() [marker.StreamIDSize]byte {
	return w.opts.StreamID
}

// Sequence returns the sequence number the next marked access unit receives.
func (w *Writer) Sequence() uint32 {
	return w.seq
}

// Mark returns a copy of the access unit with a marker inserted, in the framing
// f names. It never aliases au: an unmarked unit is copied too. at is rounded to
// microseconds. A payload above marker.PayloadSoftLimit is still written and
// reported with ErrPayloadAboveSoftLimit.
func (w *Writer) Mark(au []byte, f Format, at time.Time, payload []byte) (out []byte, marked bool, err error) {
	nalus, err := NALUnits(au, f)
	if err != nil {
		return nil, false, err
	}

	if len(nalus) == 0 {
		return nil, false, fmt.Errorf("seimark: mark access unit: %w", ErrNoVCL)
	}

	for _, nal := range nalus {
		if avc.GetNaluType(nal[0]) == avc.NALU_SEI && carriesMarker(nal) {
			return nil, false, ErrAlreadyMarked
		}
	}

	if w.opts.KeyframesOnly && !hasIDR(nalus) {
		out, err = joinNALUnits(nalus, f)

		return out, false, err
	}

	nal, err := w.markerNAL(at, payload)
	if err != nil {
		return nil, false, err
	}

	pos, err := insertIndex(nalus)
	if err != nil {
		return nil, false, err
	}

	out, err = joinNALUnits(insertAt(nalus, nal, pos), f)
	if err != nil {
		return nil, false, err
	}

	w.seq++

	if len(payload) > marker.PayloadSoftLimit {
		return out, true, fmt.Errorf("%w: %d bytes", ErrPayloadAboveSoftLimit, len(payload))
	}

	return out, true, nil
}

// markerNAL encodes the marker for this access unit into a complete SEI NAL unit.
func (w *Writer) markerNAL(at time.Time, payload []byte) ([]byte, error) {
	body, err := marker.Marker{
		TimeSource: w.opts.TimeSource,
		OriginTime: at,
		Sequence:   w.seq,
		StreamID:   w.opts.StreamID,
		Payload:    payload,
	}.Encode()
	if err != nil {
		return nil, fmt.Errorf("seimark: encode marker: %w", err)
	}

	return UserDataSEINAL(marker.FormatUUID(), body)
}

// insertIndex is where the marker NAL unit goes among nalus: after any
// delimiter, parameter sets and existing SEI, before the first VCL NAL unit.
func insertIndex(nalus [][]byte) (int, error) {
	for i, nal := range nalus {
		if avc.IsVideoNaluType(avc.GetNaluType(nal[0])) {
			return i, nil
		}
	}

	return 0, ErrNoVCL
}

// StripMarkers returns the access unit without the SEI NAL units that carry a
// seimark marker, rebuilt in the framing f names. It never aliases au.
func StripMarkers(au []byte, f Format) ([]byte, error) {
	nalus, err := NALUnits(au, f)
	if err != nil {
		return nil, err
	}

	kept := make([][]byte, 0, len(nalus))

	for _, nal := range nalus {
		if avc.GetNaluType(nal[0]) == avc.NALU_SEI && carriesMarker(nal) {
			continue
		}

		kept = append(kept, nal)
	}

	return joinNALUnits(kept, f)
}

func hasIDR(nalus [][]byte) bool {
	for _, nal := range nalus {
		if avc.GetNaluType(nal[0]) == avc.NALU_IDR {
			return true
		}
	}

	return false
}

// insertAt returns nalus with nal at index i, without touching nalus itself.
func insertAt(nalus [][]byte, nal []byte, i int) [][]byte {
	out := make([][]byte, 0, len(nalus)+1)
	out = append(out, nalus[:i]...)
	out = append(out, nal)

	return append(out, nalus[i:]...)
}

// joinNALUnits frames the NAL units into one access unit: four-byte start codes
// for Annex B, four-byte big-endian lengths otherwise. A NAL unit too long for a
// four-byte length is an error, which only a length-prefixed output can hit.
func joinNALUnits(nalus [][]byte, f Format) ([]byte, error) {
	size := 0
	for _, nal := range nalus {
		size += startCodeSize + len(nal)
	}

	out := make([]byte, 0, size)

	for _, nal := range nalus {
		if f == FormatAnnexB {
			out = append(out, fourByteStartCode()...)
			out = append(out, nal...)

			continue
		}

		n := uint64(len(nal))
		if n > math.MaxUint32 {
			return nil, fmt.Errorf("seimark: NAL unit of %d bytes does not fit a four-byte length", n)
		}

		out = binary.BigEndian.AppendUint32(out, uint32(n))
		out = append(out, nal...)
	}

	return out, nil
}
