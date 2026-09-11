package h264

import (
	"fmt"

	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/sei"
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
