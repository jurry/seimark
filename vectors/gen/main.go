// Command gen stamps an Annex B stream with seimark markers through the h264
// writer. Used only to build the fixtures in vectors/streams.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jurry/seimark/h264"
	"github.com/jurry/seimark/marker"
)

// argCount is the program name plus the input and output paths.
const argCount = 3

// expectedAccessUnits is the access-unit count of the fixture in vectors/streams.
const expectedAccessUnits = 20

// frameInterval is the fixture's 10 frames per second.
const frameInterval = 100 * time.Millisecond

// Exit codes.
const (
	exitError = 1
	exitUsage = 2
)

func main() {
	if len(os.Args) != argCount {
		fmt.Fprintln(os.Stderr, "usage: gen IN.h264 OUT.h264")
		os.Exit(exitUsage)
	}

	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(exitError)
	}
}

func run(inPath, outPath string) error {
	inPath = filepath.Clean(inPath)
	outPath = filepath.Clean(outPath)

	in, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("seimark: open %s: %w", inPath, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("seimark: create %s: %w", outPath, err)
	}

	defer func() { _ = out.Close() }()

	w, err := h264.NewWriter(h264.WriterOptions{
		StreamID: [marker.StreamIDSize]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51},
	})
	if err != nil {
		return fmt.Errorf("seimark: new writer: %w", err)
	}

	base := time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC)
	index := 0

	for au, err := range h264.AccessUnits(in) {
		if err != nil {
			return fmt.Errorf("seimark: read access unit %d: %w", index, err)
		}

		if err := markUnit(out, w, au, base, index); err != nil {
			return err
		}

		index++
	}

	if index != expectedAccessUnits {
		return fmt.Errorf("expected %d access units, got %d", expectedAccessUnits, index)
	}

	return nil
}

func markUnit(out io.Writer, w *h264.Writer, au []byte, base time.Time, index int) error {
	// The ffmpeg base stream carries no markers; stripping keeps the generator
	// right if it is ever run on a stamped stream.
	stripped, err := h264.StripMarkers(au, h264.FormatAnnexB)
	if err != nil {
		return fmt.Errorf("seimark: strip access unit %d: %w", index, err)
	}

	var payload []byte
	if index == 0 {
		payload = []byte("testsrc")
	}

	at := base.Add(time.Duration(index) * frameInterval)

	marked, _, err := w.Mark(stripped, h264.FormatAnnexB, at, payload)
	if err != nil {
		return fmt.Errorf("seimark: mark access unit %d: %w", index, err)
	}

	if _, err := out.Write(marked); err != nil {
		return fmt.Errorf("seimark: write access unit %d: %w", index, err)
	}

	return nil
}
