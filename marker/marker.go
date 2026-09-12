// Package marker implements the seimark marker body defined in docs/format.md.
package marker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Version is the only marker body version this package encodes and decodes.
const (
	Version          = 1
	PayloadSoftLimit = 4096
	PayloadHardLimit = 65535
)

// Body layout from docs/format.md. Each offset follows from the field before it.
const (
	offsetVersion    = 0
	versionSize      = 1
	offsetFlags      = offsetVersion + versionSize
	flagsSize        = 1
	offsetOriginTime = offsetFlags + flagsSize
	originTimeSize   = 8
	offsetSequence   = offsetOriginTime + originTimeSize
	sequenceSize     = 4
	offsetStreamID   = offsetSequence + sequenceSize
	// StreamIDSize is the length of Marker.StreamID.
	StreamIDSize        = 8
	offsetPayloadLength = offsetStreamID + StreamIDSize
	payloadLengthSize   = 2
	offsetPayload       = offsetPayloadLength + payloadLengthSize
)

// FixedSize is the length of the body before the optional payload length and payload.
const FixedSize = offsetPayloadLength

// UUIDSize is the length of the format UUID.
const UUIDSize = 16

// FormatUUID identifies a seimark marker inside a user_data_unregistered SEI message.
func FormatUUID() [UUIDSize]byte {
	return [UUIDSize]byte{0x44, 0xa7, 0x3c, 0xb9, 0xb3, 0x6c, 0x45, 0x9a, 0x8f, 0x1a, 0xa3, 0xaa, 0x43, 0x1f, 0x62, 0x4a}
}

// TimeSource says which event OriginTime records.
type TimeSource uint8

// TimeSend means OriginTime is when the marker was sent.
// TimeCapture means OriginTime is when the frame was captured.
const (
	TimeSend    TimeSource = 0
	TimeCapture TimeSource = 1
)

func (s TimeSource) String() string {
	switch s {
	case TimeSend:
		return "send"
	case TimeCapture:
		return "capture"
	}

	return fmt.Sprintf("time_source(%d)", uint8(s))
}

// Marker is one decoded marker. Payload is nil when the marker carries none and
// non-nil, possibly empty, when the payload flag is set.
type Marker struct {
	TimeSource TimeSource
	OriginTime time.Time
	Sequence   uint32
	StreamID   [StreamIDSize]byte
	Payload    []byte
}

// ErrUnsupportedVersion is returned when a marker body's version byte is not Version.
var (
	ErrUnsupportedVersion = errors.New("seimark: unsupported marker version")
	ErrTruncated          = errors.New("seimark: truncated marker body")
	ErrPayloadTooLarge    = errors.New("seimark: payload exceeds 65535 bytes")
)

const (
	flagTimeCapture = 1 << 0
	flagPayload     = 1 << 1
)

// Decode parses a marker body. Reserved flag bits and bytes after the body are ignored.
func Decode(body []byte) (Marker, error) {
	if len(body) < FixedSize {
		return Marker{}, fmt.Errorf("%w: %d bytes, need %d", ErrTruncated, len(body), FixedSize)
	}

	if body[0] != Version {
		return Marker{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, body[0])
	}

	flags := body[offsetFlags]

	m := Marker{
		OriginTime: time.UnixMicro(readInt64(body[offsetOriginTime : offsetOriginTime+originTimeSize])).UTC(),
		Sequence:   binary.BigEndian.Uint32(body[offsetSequence : offsetSequence+sequenceSize]),
	}
	if flags&flagTimeCapture != 0 {
		m.TimeSource = TimeCapture
	}

	copy(m.StreamID[:], body[offsetStreamID:offsetStreamID+StreamIDSize])

	if flags&flagPayload != 0 {
		if len(body) < offsetPayload {
			return Marker{}, fmt.Errorf("%w: payload flag set but no length", ErrTruncated)
		}

		n := int(binary.BigEndian.Uint16(body[offsetPayloadLength:offsetPayload]))
		if len(body) < offsetPayload+n {
			return Marker{}, fmt.Errorf("%w: payload length %d, %d bytes left", ErrTruncated, n, len(body)-offsetPayload)
		}

		m.Payload = append([]byte{}, body[offsetPayload:offsetPayload+n]...)
	}

	return m, nil
}

// readInt64 reads eight big-endian bytes; docs/format.md defines the origin
// time as a signed 64-bit value, so the two's-complement reinterpretation is
// the specified conversion and cannot overflow.
func readInt64(b []byte) int64 {
	//nolint:gosec // G115: docs/format.md defines the origin time as signed 64-bit; this is the two's-complement read.
	return int64(binary.BigEndian.Uint64(b))
}

// IsFormatUUID reports whether uuid is the seimark format UUID.
func IsFormatUUID(uuid []byte) bool {
	return len(uuid) == UUIDSize && [UUIDSize]byte(uuid) == FormatUUID()
}

// Encode returns the marker body. The payload flag is set when Payload is non-nil.
// OriginTime is rounded to microseconds.
func (m Marker) Encode() ([]byte, error) {
	if len(m.Payload) > PayloadHardLimit {
		return nil, fmt.Errorf("%w: %d", ErrPayloadTooLarge, len(m.Payload))
	}

	out := make([]byte, FixedSize, offsetPayload+len(m.Payload))

	out[offsetVersion] = Version
	if m.TimeSource == TimeCapture {
		out[offsetFlags] |= flagTimeCapture
	}

	binary.BigEndian.PutUint64(out[offsetOriginTime:offsetOriginTime+originTimeSize], uint64(m.OriginTime.UnixMicro()))
	binary.BigEndian.PutUint32(out[offsetSequence:offsetSequence+sequenceSize], m.Sequence)
	copy(out[offsetStreamID:offsetStreamID+StreamIDSize], m.StreamID[:])

	if m.Payload != nil {
		n := len(m.Payload)
		if n > PayloadHardLimit {
			return nil, fmt.Errorf("%w: %d", ErrPayloadTooLarge, n)
		}

		out[offsetFlags] |= flagPayload
		out = binary.BigEndian.AppendUint16(out, uint16(n))
		out = append(out, m.Payload...)
	}

	return out, nil
}
