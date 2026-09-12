package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/Eyevinn/mp4ff/avc"

	"github.com/jurry/seimark/h264"
	"github.com/jurry/seimark/marker"
)

const (
	placementBeforeVCL = "before-vcl"
	placementAppend    = "append"
	startNow           = "now"
)

// streamIDHexLength is the stream id as -stream-id takes it: eight bytes of hex.
const streamIDHexLength = 2 * marker.StreamIDSize

// fieldsPerFrameRate is the 2 in time_scale / (2 * num_units_in_tick), which
// counts the two fields of a frame.
const fieldsPerFrameRate = 2

// injectFlags holds runInject's parsed and validated flags.
type injectFlags struct {
	start   time.Time
	fps     float64
	opts    h264.WriterOptions
	inPath  string
	outPath string
}

// parseInjectFlags parses and validates args, writing the problem to stderr and
// returning ok=false when args are unusable.
func parseInjectFlags(args []string, stderr io.Writer) (flags injectFlags, exitCode int, ok bool) {
	fs := flag.NewFlagSet("inject", flag.ContinueOnError)
	fs.SetOutput(stderr)
	start := fs.String("start", startNow, "origin time of the first access unit: RFC 3339 or now")
	fps := fs.Float64("fps", 0, "frame rate; without it the SPS VUI timing decides")
	streamID := fs.String("stream-id", "", "stream id as 16 hex characters; without it eight random bytes")
	placement := fs.String("placement", placementBeforeVCL, "marker placement: before-vcl or append")
	keyframesOnly := fs.Bool("keyframes-only", false, "mark only access units with an IDR picture")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return injectFlags{}, exitOK, false
		}

		return injectFlags{}, exitUsage, false
	}

	const pathArgs = 2
	if fs.NArg() != pathArgs {
		fmt.Fprintln(stderr, "seimark inject: exactly one IN and one OUT are required")

		return injectFlags{}, exitUsage, false
	}

	flags = injectFlags{fps: *fps, inPath: fs.Arg(0), outPath: fs.Arg(1)}

	flags.opts.KeyframesOnly = *keyframesOnly

	var err error
	if flags.start, err = parseStart(*start); err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return injectFlags{}, exitUsage, false
	}

	if flags.opts.Placement, err = parsePlacement(*placement); err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return injectFlags{}, exitUsage, false
	}

	if flags.opts.StreamID, err = parseStreamID(*streamID); err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return injectFlags{}, exitUsage, false
	}

	if *fps < 0 {
		fmt.Fprintf(stderr, "seimark inject: -fps must be positive, got %v\n", *fps)

		return injectFlags{}, exitUsage, false
	}

	return flags, exitOK, true
}

func parseStart(s string) (time.Time, error) {
	if s == startNow {
		return time.Now().UTC(), nil
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("-start must be RFC 3339 or now: %w", err)
	}

	return t.UTC(), nil
}

func parsePlacement(s string) (h264.Placement, error) {
	switch s {
	case placementBeforeVCL:
		return h264.PlacementBeforeVCL, nil
	case placementAppend:
		return h264.PlacementAppend, nil
	}

	return 0, fmt.Errorf("-placement must be %s or %s, got %q", placementBeforeVCL, placementAppend, s)
}

// parseStreamID returns the zero id for an empty flag, which makes the writer
// draw a random one.
func parseStreamID(s string) ([marker.StreamIDSize]byte, error) {
	var id [marker.StreamIDSize]byte

	if s == "" {
		return id, nil
	}

	if len(s) != streamIDHexLength {
		return id, fmt.Errorf("-stream-id must be %d hex characters, got %d", streamIDHexLength, len(s))
	}

	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("-stream-id is not hex: %w", err)
	}

	copy(id[:], b)

	return id, nil
}

func runInject(args []string, _, stderr io.Writer) int {
	flags, exitCode, ok := parseInjectFlags(args, stderr)
	if !ok {
		return exitCode
	}

	in, err := os.Open(flags.inPath)
	if err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return exitError
	}

	defer func() { _ = in.Close() }()

	format, err := sniff(in)
	switch {
	case errors.Is(err, errUnknownInput):
		fmt.Fprintf(stderr, "seimark inject: %v; inject reads Annex B streams\n", err)

		return exitUsage
	case err != nil:
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return exitError
	case format != formatAnnexB:
		fmt.Fprintln(stderr, "seimark inject: inject reads Annex B streams; MP4 input comes in a later phase")

		return exitUsage
	}

	rate := flags.fps
	if rate == 0 {
		if rate, err = rateFromSPS(in); err != nil {
			fmt.Fprintf(stderr, "seimark inject: %v\n", err)

			return exitUsage
		}
	}

	return writeMarked(in, &flags, rate, stderr)
}

// errNoRate is the stream that names no frame rate of its own.
var errNoRate = errors.New("no frame rate in the stream; pass -fps")

// rateFromSPS reads the frame rate out of the first SPS's VUI timing, then
// rewinds. A stream whose SPS does not parse or carries no timing has no rate.
func rateFromSPS(in io.ReadSeeker) (float64, error) {
	defer func() { _, _ = in.Seek(0, io.SeekStart) }()

	for au, err := range h264.AccessUnits(in) {
		if err != nil {
			return 0, fmt.Errorf("seimark: read access unit: %w", err)
		}

		nalus, err := h264.NALUnits(au, h264.FormatAnnexB)
		if err != nil {
			return 0, fmt.Errorf("seimark: split access unit: %w", err)
		}

		for _, nal := range nalus {
			if avc.GetNaluType(nal[0]) != avc.NALU_SPS {
				continue
			}

			sps, err := avc.ParseSPSNALUnit(nal, true)
			if err != nil || sps.VUI == nil || !sps.VUI.TimingInfoPresentFlag || sps.VUI.NumUnitsInTick == 0 {
				return 0, errNoRate
			}

			return float64(sps.VUI.TimeScale) / (fieldsPerFrameRate * float64(sps.VUI.NumUnitsInTick)), nil
		}

		break
	}

	return 0, errNoRate
}

// markOne marks one access unit, reporting to stderr and returning ok=false on
// an error that should stop the run. A unit without a picture is passed through.
func markOne(w *h264.Writer, au []byte, at time.Time, index int, stderr io.Writer) ([]byte, bool) {
	marked, _, err := w.Mark(au, h264.FormatAnnexB, at, nil)

	switch {
	case errors.Is(err, h264.ErrAlreadyMarked):
		fmt.Fprintln(stderr, "seimark inject: input already carries seimark markers")

		return nil, false
	case errors.Is(err, h264.ErrNoVCL):
		fmt.Fprintf(stderr, "seimark inject: access unit %d has no picture; left unmarked\n", index)

		return au, true
	case err != nil:
		fmt.Fprintf(stderr, "seimark inject: access unit %d: %v\n", index, err)

		return nil, false
	}

	return marked, true
}

// writeMarked stamps every access unit of in into the output file.
func writeMarked(in io.Reader, flags *injectFlags, rate float64, stderr io.Writer) int {
	w, err := h264.NewWriter(flags.opts)
	if err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return exitError
	}

	out, err := os.Create(flags.outPath)
	if err != nil {
		fmt.Fprintf(stderr, "seimark inject: %v\n", err)

		return exitError
	}

	defer func() { _ = out.Close() }()

	buf := bufio.NewWriter(out)
	index := 0

	for au, err := range h264.AccessUnits(in) {
		if err != nil {
			fmt.Fprintf(stderr, "seimark inject: %v\n", err)

			return exitError
		}

		at := flags.start.Add(time.Duration(math.Round(float64(index) * float64(time.Second) / rate)))

		marked, ok := markOne(w, au, at, index, stderr)
		if !ok {
			return exitError
		}

		if _, err := buf.Write(marked); err != nil {
			fmt.Fprintf(stderr, "seimark inject: write: %v\n", err)

			return exitError
		}

		index++
	}

	if err := buf.Flush(); err != nil {
		fmt.Fprintf(stderr, "seimark inject: write: %v\n", err)

		return exitError
	}

	return exitOK
}
