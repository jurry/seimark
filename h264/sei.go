package h264

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/sei"

	"github.com/jurry/seimark/marker"
)

// UserDataSEINAL builds a complete SEI NAL unit, header byte and emulation
// prevention included, carrying one user_data_unregistered message.
func UserDataSEINAL(uuid [marker.UUIDSize]byte, body []byte) ([]byte, error) {
	payload := make([]byte, 0, len(uuid)+len(body))
	payload = append(payload, uuid[:]...)
	payload = append(payload, body...)

	nal, err := avc.CreateSEINalu([]sei.SEIMessage{sei.NewSEIData(sei.SEIUserDataUnregisteredType, payload)})
	if err != nil {
		return nil, fmt.Errorf("seimark: build SEI NAL unit: %w", err)
	}

	return nal, nil
}

// ErrUnparsableSEI marks an SEI NAL unit that could not be parsed at all. It is
// advisory: the markers found elsewhere in the access unit are returned with it,
// and a caller that only wants those can ignore an error that is this one.
var ErrUnparsableSEI = errors.New("seimark: SEI NAL unit does not parse")

// SEIMessage is one message of one SEI NAL unit, classified far enough for a
// caller to report it: the payload type, the unregistered UUID when there is
// one, and the decoded seimark marker when the UUID is seimark's and the body
// decodes.
type SEIMessage struct {
	Type    uint
	UUID    [marker.UUIDSize]byte
	HasUUID bool
	Marker  *marker.Marker
	Payload []byte
}

// SEIMessages returns the messages of one SEI NAL unit, header byte included.
// A NAL unit that does not parse at all is ErrUnparsableSEI. A seimark message
// whose body does not decode ends the walk with that error and the messages
// found so far.
func SEIMessages(nal []byte) ([]SEIMessage, error) {
	if len(nal) < naluHeaderSize+1 {
		return nil, fmt.Errorf("%w: %d bytes", ErrUnparsableSEI, len(nal))
	}

	msgs, err := sei.ExtractSEIData(bytes.NewReader(nal[naluHeaderSize:]))
	if err != nil && len(msgs) == 0 {
		return nil, fmt.Errorf("%w: %w", ErrUnparsableSEI, err)
	}

	out := make([]SEIMessage, 0, len(msgs))

	for i := range msgs {
		m, err := classifySEIMessage(&msgs[i])
		if err != nil {
			return out, err
		}

		out = append(out, m)
	}

	return out, nil
}

// classifySEIMessage fills in the UUID and the marker of one message.
func classifySEIMessage(msg *sei.SEIData) (SEIMessage, error) {
	payload := msg.Payload()
	out := SEIMessage{Type: msg.Type(), Payload: payload}

	if msg.Type() != sei.SEIUserDataUnregisteredType || len(payload) < marker.UUIDSize {
		return out, nil
	}

	copy(out.UUID[:], payload[:marker.UUIDSize])

	out.HasUUID = true
	if !marker.IsFormatUUID(payload[:marker.UUIDSize]) {
		return out, nil
	}

	m, err := marker.Decode(payload[marker.UUIDSize:])
	if err != nil {
		return out, fmt.Errorf("seimark: marker in SEI NAL unit: %w", err)
	}

	out.Marker = &m

	return out, nil
}

// Markers returns every seimark marker in the access unit, in order.
// Unregistered messages with other UUIDs are skipped. An SEI NAL unit that does
// not parse is skipped too, but the scan ends with ErrUnparsableSEI alongside
// the markers found. A message with the seimark UUID that does not decode ends
// the scan at once: the markers found so far are returned with that error.
func Markers(au []byte, f Format) ([]marker.Marker, error) {
	nalus, err := NALUnits(au, f)
	if err != nil {
		return nil, err
	}

	var (
		found      []marker.Marker
		unparsable error
	)

	for _, nal := range nalus {
		if len(nal) == 0 || avc.GetNaluType(nal[0]) != avc.NALU_SEI {
			continue
		}

		msgs, err := SEIMessages(nal)
		if errors.Is(err, ErrUnparsableSEI) {
			if unparsable == nil {
				unparsable = err
			}

			continue
		}

		for _, msg := range msgs {
			if msg.Marker != nil {
				found = append(found, *msg.Marker)
			}
		}

		if err != nil {
			return found, err
		}
	}

	return found, unparsable
}
