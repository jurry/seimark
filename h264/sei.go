package h264

import (
	"bytes"
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

// Markers returns every seimark marker in the access unit, in order. SEI NAL
// units that do not parse and unregistered messages with other UUIDs are
// skipped. A message with the seimark UUID that does not decode ends the scan:
// the markers found so far are returned together with the error.
func Markers(au []byte, f Format) ([]marker.Marker, error) {
	nalus, err := NALUnits(au, f)
	if err != nil {
		return nil, err
	}
	var found []marker.Marker
	for _, nal := range nalus {
		if len(nal) < 2 || avc.GetNaluType(nal[0]) != avc.NALU_SEI {
			continue
		}
		msgs, err := sei.ExtractSEIData(bytes.NewReader(nal[1:]))
		if err != nil && len(msgs) == 0 {
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
	return found, nil
}
