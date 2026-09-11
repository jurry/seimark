// Command gen inserts a seimark marker before the first VCL NAL unit of every
// access unit of an Annex B stream. Used only to build the fixtures in
// vectors/streams; the phase 2 writer supersedes it.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Eyevinn/mp4ff/avc"

	"github.com/jurry/seimark/h264"
	"github.com/jurry/seimark/marker"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen IN.h264 OUT.h264")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath string) error {
	in, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	base := time.Date(2026, 9, 11, 21, 0, 0, 0, time.UTC)
	streamID := [8]byte{0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51}
	index := 0
	for au, err := range h264.AccessUnits(in) {
		if err != nil {
			return err
		}
		m := marker.Marker{
			OriginTime: base.Add(time.Duration(index) * 100 * time.Millisecond),
			Sequence:   uint32(index),
			StreamID:   streamID,
		}
		if index == 0 {
			m.Payload = []byte("testsrc")
		}
		body, err := m.Encode()
		if err != nil {
			return err
		}
		nal, err := h264.UserDataSEINAL(marker.FormatUUID, body)
		if err != nil {
			return err
		}
		if err := writeMarked(out, au, nal); err != nil {
			return err
		}
		index++
	}
	if index != 20 {
		return fmt.Errorf("expected 20 access units, got %d", index)
	}
	return nil
}

// writeMarked writes the access unit with the marker inserted before its first VCL NAL unit.
func writeMarked(w io.Writer, au, markerNAL []byte) error {
	nalus, err := h264.NALUnits(au, h264.FormatAnnexB)
	if err != nil {
		return err
	}
	inserted := false
	for _, nal := range nalus {
		if !inserted && avc.IsVideoNaluType(avc.GetNaluType(nal[0])) {
			if err := writeNAL(w, markerNAL); err != nil {
				return err
			}
			inserted = true
		}
		if err := writeNAL(w, nal); err != nil {
			return err
		}
	}
	if !inserted {
		return fmt.Errorf("access unit without a VCL NAL unit")
	}
	return nil
}

func writeNAL(w io.Writer, nal []byte) error {
	if _, err := w.Write([]byte{0, 0, 0, 1}); err != nil {
		return err
	}
	_, err := w.Write(nal)
	return err
}
