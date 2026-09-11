package marker

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type markerJSON struct {
	Version    int     `json:"version"`
	TimeSource string  `json:"time_source"`
	OriginTime string  `json:"origin_time"`
	OriginUS   int64   `json:"origin_us"`
	Sequence   uint32  `json:"sequence"`
	StreamID   string  `json:"stream_id"`
	Payload    *string `json:"payload"`
	Error      string  `json:"error"`
}

func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrUnsupportedVersion):
		return "unsupported_version"
	case errors.Is(err, ErrTruncated):
		return "truncated"
	}
	return "other: " + err.Error()
}

func TestVectors(t *testing.T) {
	bins, err := filepath.Glob(filepath.Join("..", "vectors", "markers", "*.bin"))
	if err != nil || len(bins) == 0 {
		t.Fatalf("no vectors found: %v", err)
	}
	for _, bin := range bins {
		name := strings.TrimSuffix(filepath.Base(bin), ".bin")
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(bin)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(bin, ".bin") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want markerJSON
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			got, err := Decode(body)
			if want.Error != "" {
				if code := errorCode(err); code != want.Error {
					t.Fatalf("error = %q (%v), want %q", code, err, want.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.TimeSource.String() != want.TimeSource {
				t.Errorf("time_source = %s, want %s", got.TimeSource, want.TimeSource)
			}
			if s := got.OriginTime.UTC().Format("2006-01-02T15:04:05.000000Z07:00"); s != want.OriginTime {
				t.Errorf("origin_time = %s, want %s", s, want.OriginTime)
			}
			if got.OriginTime.UnixMicro() != want.OriginUS {
				t.Errorf("origin_us = %d, want %d", got.OriginTime.UnixMicro(), want.OriginUS)
			}
			if got.Sequence != want.Sequence {
				t.Errorf("sequence = %d, want %d", got.Sequence, want.Sequence)
			}
			if s := hexString(got.StreamID[:]); s != want.StreamID {
				t.Errorf("stream_id = %s, want %s", s, want.StreamID)
			}
			switch {
			case want.Payload == nil && got.Payload != nil:
				t.Errorf("payload = %x, want none", got.Payload)
			case want.Payload != nil && got.Payload == nil:
				t.Errorf("payload missing, want %s", *want.Payload)
			case want.Payload != nil:
				if s := base64.StdEncoding.EncodeToString(got.Payload); s != *want.Payload {
					t.Errorf("payload = %s, want %s", s, *want.Payload)
				}
			}
			// Positive vectors must round-trip byte for byte.
			enc, err := got.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(enc) != string(body[:len(enc)]) {
				t.Errorf("re-encoded body differs:\n got %x\nwant %x", enc, body)
			}
		})
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&0x0f])
	}
	return string(out)
}
