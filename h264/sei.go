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
func UserDataSEINAL(uuid [16]byte, body []byte) ([]byte, error) {
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
		if len(nal) < 2 || avc.GetNaluType(nal[0]) != avc.NALU_SEI {
			continue
		}
		msgs, err := sei.ExtractSEIData(bytes.NewReader(nal[1:]))
		if err != nil && len(msgs) == 0 {
			if unparsable == nil {
				unparsable = fmt.Errorf("%w: %w", ErrUnparsableSEI, err)
			}
			continue
		}
		for _, msg := range msgs {
			payload := msg.Payload()
			if msg.Type() != sei.SEIUserDataUnregisteredType || len(payload) < 16 || !marker.IsFormatUUID(payload[:16]) {
				continue
			}
			m, err := marker.Decode(payload[16:])
			if err != nil {
				return found, fmt.Errorf("seimark: marker in SEI NAL unit: %w", err)
			}
			found = append(found, m)
		}
	}
	return found, unparsable
}
