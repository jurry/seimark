package h264

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jurry/seimark/marker"
)

// The committed Annex B fixture must be exactly what the writer produces from
// its own stripped NAL units with the generator's parameters.
func TestFixtureIsWriterOutput(t *testing.T) {
	t.Parallel()

	fixture, err := os.ReadFile(filepath.Join("..", "vectors", "streams", "testsrc-marked.h264"))
	if err != nil {
		t.Fatal(err)
	}

	w, err := NewWriter(WriterOptions{StreamID: [marker.StreamIDSize]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51}})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC)

	var rebuilt []byte

	index := 0

	for au, err := range AccessUnits(bytes.NewReader(fixture)) {
		if err != nil {
			t.Fatal(err)
		}

		stripped, err := StripMarkers(au, FormatAnnexB)
		if err != nil {
			t.Fatal(err)
		}

		var payload []byte
		if index == 0 {
			payload = []byte("testsrc")
		}

		out, marked, err := w.Mark(stripped, FormatAnnexB, base.Add(time.Duration(index)*100*time.Millisecond), payload)
		if err != nil || !marked {
			t.Fatalf("unit %d: marked=%v err=%v", index, marked, err)
		}

		rebuilt = append(rebuilt, out...)
		index++
	}

	if index != 20 {
		t.Fatalf("%d access units, want 20", index)
	}

	if !bytes.Equal(rebuilt, fixture) {
		t.Fatal("rebuilt stream differs from the committed fixture")
	}
}
