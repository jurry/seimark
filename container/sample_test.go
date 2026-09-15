package container

import (
	"testing"

	"github.com/jurry/seimark/h264"
)

func TestFramingH264(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		framing Framing
		want    h264.Format
	}{
		{FramingLengthPrefixed, h264.FormatLengthPrefixed},
		{FramingAnnexB, h264.FormatAnnexB},
	} {
		if got := tc.framing.H264(); got != tc.want {
			t.Errorf("Framing(%d).H264() = %v, want %v", tc.framing, got, tc.want)
		}
	}
}

func TestFramingString(t *testing.T) {
	t.Parallel()

	if got := FramingLengthPrefixed.String(); got != "length-prefixed" {
		t.Errorf("FramingLengthPrefixed.String() = %q", got)
	}

	if got := FramingAnnexB.String(); got != "annexb" {
		t.Errorf("FramingAnnexB.String() = %q", got)
	}
}
