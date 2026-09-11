package h264

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNALVectors(t *testing.T) {
	bins, err := filepath.Glob(filepath.Join("..", "vectors", "nal", "*.bin"))
	if err != nil || len(bins) == 0 {
		t.Fatalf("no vectors found: %v", err)
	}
	for _, bin := range bins {
		name := strings.TrimSuffix(filepath.Base(bin), ".bin")
		t.Run(name, func(t *testing.T) {
			nal, err := os.ReadFile(bin)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(bin, ".bin") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want []struct {
				Sequence uint32 `json:"sequence"`
				OriginUS int64  `json:"origin_us"`
				StreamID string `json:"stream_id"`
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			got, err := Markers(annexB(nal), FormatAnnexB)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d markers, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i].Sequence != want[i].Sequence || got[i].OriginTime.UnixMicro() != want[i].OriginUS ||
					unhexString(got[i].StreamID[:]) != want[i].StreamID {
					t.Errorf("marker %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

func unhexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&0x0f])
	}
	return string(out)
}
