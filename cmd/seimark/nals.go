package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Eyevinn/mp4ff/avc"

	"github.com/jurry/seimark/h264"
	"github.com/jurry/seimark/mp4"
)

// originTimeLayout is RFC 3339 with six fractional digits, as dump and nals
// both print an origin time.
const originTimeLayout = "2006-01-02T15:04:05.000000Z07:00"

// parseNalsFlags parses and validates args the way parseDumpFlags does, minus
// the output options nals has no use for.
func parseNalsFlags(args []string, stderr io.Writer) (format, path string, exitCode int, ok bool) {
	fs := flag.NewFlagSet("nals", flag.ContinueOnError)
	fs.SetOutput(stderr)

	f := fs.String("format", formatAuto, "input format: auto, annexb or mp4")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", "", exitOK, false
		}

		return "", "", exitUsage, false
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "seimark nals: exactly one FILE is required")

		return "", "", exitUsage, false
	}

	if *f != formatAuto && *f != formatAnnexB && *f != formatMP4 {
		fmt.Fprintf(stderr, "seimark nals: -format must be auto, annexb or mp4, got %q\n", *f)

		return "", "", exitUsage, false
	}

	return *f, fs.Arg(0), exitOK, true
}

func runNals(args []string, stdout, stderr io.Writer) int {
	format, path, exitCode, ok := parseNalsFlags(args, stderr)
	if !ok {
		return exitCode
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "seimark nals: %v\n", err)

		return exitError
	}

	defer func() { _ = f.Close() }()

	if format == formatAuto {
		format, err = sniff(f)
		switch {
		case errors.Is(err, errUnknownInput):
			fmt.Fprintf(stderr, "seimark nals: %v; pass -format\n", err)

			return exitUsage
		case err != nil:
			fmt.Fprintf(stderr, "seimark nals: %v\n", err)

			return exitError
		}
	}

	w := bufio.NewWriter(stdout)

	var walkErr error

	switch format {
	case formatAnnexB:
		walkErr = nalsAnnexB(f, w)
	case formatMP4:
		walkErr = nalsMP4(f, w)
	}

	if err := w.Flush(); err != nil {
		fmt.Fprintf(stderr, "seimark nals: write: %v\n", err)

		return exitError
	}

	if walkErr != nil {
		fmt.Fprintf(stderr, "seimark nals: %v\n", walkErr)

		return exitError
	}

	return exitOK
}

func nalsAnnexB(r io.Reader, w io.Writer) error {
	index := 0

	for au, err := range h264.AccessUnits(r) {
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "au %d\n", index)

		if err := printNALUnits(w, au, h264.FormatAnnexB); err != nil {
			return err
		}

		index++
	}

	return nil
}

func nalsMP4(r io.ReadSeeker, w io.Writer) error {
	for s, err := range mp4.VideoSamples(r) {
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "au %d dts %d pts %d\n", s.Index, s.DTS, s.PTS)

		if err := printNALUnits(w, s.Data, s.Framing.H264()); err != nil {
			return err
		}
	}

	return nil
}

func printNALUnits(w io.Writer, au []byte, f h264.Format) error {
	nalus, err := h264.NALUnits(au, f)
	if err != nil {
		return fmt.Errorf("seimark: split access unit: %w", err)
	}

	for _, nal := range nalus {
		t := avc.GetNaluType(nal[0])

		fmt.Fprintf(w, "  %s %d bytes", t, len(nal))

		if t == avc.NALU_SEI {
			fmt.Fprintf(w, " %s", seiSummary(nal))
		}

		fmt.Fprintln(w)
	}

	return nil
}

func seiSummary(nal []byte) string {
	msgs, err := h264.SEIMessages(nal)
	if errors.Is(err, h264.ErrUnparsableSEI) {
		return "unparsable"
	}

	parts := make([]string, 0, len(msgs)+1)
	for i := range msgs {
		parts = append(parts, messageSummary(&msgs[i]))
	}

	if err != nil {
		parts = append(parts, fmt.Sprintf("seimark undecodable: %v", err))
	}

	return strings.Join(parts, " ")
}

// messageSummary names one SEI message: a decoded seimark marker, a foreign
// unregistered UUID, or the bare message type.
func messageSummary(msg *h264.SEIMessage) string {
	if !msg.HasUUID {
		return fmt.Sprintf("type=%d", msg.Type)
	}

	if msg.Marker == nil {
		return "user_data_unregistered uuid=" + hex.EncodeToString(msg.UUID[:])
	}

	m := msg.Marker

	return fmt.Sprintf("seimark seq=%d time=%s stream=%s payload=%d",
		m.Sequence, m.OriginTime.UTC().Format(originTimeLayout), hex.EncodeToString(m.StreamID[:]), len(m.Payload))
}
