package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/jurry/seimark/h264"
	"github.com/jurry/seimark/marker"
	"github.com/jurry/seimark/mp4"
)

// record is one output line. Pointer fields are omitted when nil, which is how
// Annex B input (no container timing) and unmarked access units are expressed.
type record struct {
	AU          int      `json:"au"`
	DTS         *uint64  `json:"dts,omitempty"`
	PTS         *int64   `json:"pts,omitempty"`
	Timescale   *uint32  `json:"timescale,omitempty"`
	Sync        *bool    `json:"sync,omitempty"`
	Time        *float64 `json:"time,omitempty"`
	MarkerIndex *int     `json:"marker_index,omitempty"`
	Version     *int     `json:"version,omitempty"`
	TimeSource  string   `json:"time_source,omitempty"`
	OriginTime  string   `json:"origin_time,omitempty"`
	OriginUS    *int64   `json:"origin_us,omitempty"`
	Sequence    *uint32  `json:"sequence,omitempty"`
	StreamID    string   `json:"stream_id,omitempty"`
	Payload     *string  `json:"payload,omitempty"`
}

// errUnknownInput marks a file whose first bytes are neither MP4 nor Annex B,
// which is a usage problem and not an I/O failure.
var errUnknownInput = errors.New("cannot tell the input format from its first bytes")

const (
	formatAuto   = "auto"
	formatAnnexB = "annexb"
	formatMP4    = "mp4"
	outJSONL     = "jsonl"
	outCSV       = "csv"
)

func csvHeader() []string {
	return []string{
		"au", "dts", "pts", "timescale", "sync", "time", "marker_index",
		"version", "time_source", "origin_time", "origin_us", "sequence", "stream_id", "payload",
	}
}

// dumpFlags holds runDump's parsed and validated flags.
type dumpFlags struct {
	format string
	out    string
	all    bool
	path   string
}

// parseDumpFlags parses and validates args, writing a usage or validation
// error to stderr and returning ok=false when args are unusable.
func parseDumpFlags(args []string, stderr io.Writer) (flags dumpFlags, exitCode int, ok bool) {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", formatAuto, "input format: auto, annexb or mp4")
	out := fs.String("out", outJSONL, "output format: jsonl or csv")

	all := fs.Bool("all", false, "also print access units without a marker")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return dumpFlags{}, exitOK, false
		}

		return dumpFlags{}, exitUsage, false
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "seimark dump: exactly one FILE is required")

		return dumpFlags{}, exitUsage, false
	}

	if *out != outJSONL && *out != outCSV {
		fmt.Fprintf(stderr, "seimark dump: -out must be jsonl or csv, got %q\n", *out)

		return dumpFlags{}, exitUsage, false
	}

	if *format != formatAuto && *format != formatAnnexB && *format != formatMP4 {
		fmt.Fprintf(stderr, "seimark dump: -format must be auto, annexb or mp4, got %q\n", *format)

		return dumpFlags{}, exitUsage, false
	}

	return dumpFlags{format: *format, out: *out, all: *all, path: fs.Arg(0)}, exitOK, true
}

func runDump(args []string, stdout, stderr io.Writer) int {
	flags, exitCode, ok := parseDumpFlags(args, stderr)
	if !ok {
		return exitCode
	}

	f, err := os.Open(flags.path)
	if err != nil {
		fmt.Fprintf(stderr, "seimark dump: %v\n", err)

		return exitError
	}

	defer func() { _ = f.Close() }()

	format := flags.format
	if format == formatAuto {
		format, err = sniff(f)
		switch {
		case errors.Is(err, errUnknownInput):
			fmt.Fprintf(stderr, "seimark dump: %v; pass -format\n", err)

			return exitUsage
		case err != nil:
			fmt.Fprintf(stderr, "seimark dump: %v\n", err)

			return exitError
		}
	}

	w := newRecordWriter(flags.out, stdout)
	warn := func(au int, err error) {
		fmt.Fprintf(stderr, "seimark dump: access unit %d: %v\n", au, err)
	}

	var walkErr error

	switch format {
	case formatAnnexB:
		walkErr = dumpAnnexB(f, w, flags.all, warn)
	case formatMP4:
		walkErr = dumpMP4(f, w, flags.all, warn)
	}

	if err := w.flush(); err != nil {
		fmt.Fprintf(stderr, "seimark dump: write: %v\n", err)

		return exitError
	}

	if walkErr != nil {
		fmt.Fprintf(stderr, "seimark dump: %v\n", walkErr)

		return exitError
	}

	return exitOK
}

// boxSizeFieldSize is the width of an ISO BMFF box's leading size field, where its type field begins.
const boxSizeFieldSize = 4

// boxHeaderSize is a box's size field plus its four-byte type field.
const boxHeaderSize = boxSizeFieldSize + 4

// sniffHeadSize is the read-ahead used to tell mp4 from Annex B: enough for a
// box header and for h264.DetectFormat's own look at the first bytes.
const sniffHeadSize = 12

// sniff decides between mp4 and annexb from the first bytes and rewinds. A
// short file is not an error; anything else the read reports is.
func sniff(f io.ReadSeeker) (string, error) {
	var head [sniffHeadSize]byte

	n, err := io.ReadFull(f, head[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("seimark: read input: %w", err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seimark: rewind input: %w", err)
	}

	if n >= boxHeaderSize {
		switch string(head[boxSizeFieldSize:boxHeaderSize]) {
		case "ftyp", "moov", "moof", "styp":
			return formatMP4, nil
		}
	}

	if h264.DetectFormat(head[:n]) == h264.FormatAnnexB {
		return formatAnnexB, nil
	}

	return "", errUnknownInput
}

func dumpAnnexB(r io.Reader, w *recordWriter, all bool, warn func(int, error)) error {
	index := 0

	for au, err := range h264.AccessUnits(r) {
		if err != nil {
			return err
		}

		emit(w, &record{AU: index}, au, h264.FormatAnnexB, all, warn)
		index++
	}

	return nil
}

func dumpMP4(r io.ReadSeeker, w *recordWriter, all bool, warn func(int, error)) error {
	for s, err := range mp4.VideoSamples(r) {
		if err != nil {
			return err
		}

		dts, pts, ts, sync := s.DTS, s.PTS, s.Timescale, s.Sync
		t := float64(pts) / float64(ts)
		base := record{AU: s.Index, DTS: &dts, PTS: &pts, Timescale: &ts, Sync: &sync, Time: &t}
		emit(w, &base, s.Data, s.Framing.H264(), all, warn)
	}

	return nil
}

// emit writes one record per marker in the access unit, or the bare record when
// all is set and no marker was found.
func emit(w *recordWriter, base *record, au []byte, f h264.Format, all bool, warn func(int, error)) {
	markers, err := h264.Markers(au, f)
	if err != nil {
		warn(base.AU, err)
	}

	if len(markers) == 0 {
		if all {
			w.write(base)
		}

		return
	}

	for i, m := range markers {
		rec := *base
		v := marker.Version
		us := m.OriginTime.UnixMicro()
		seq := m.Sequence
		rec.MarkerIndex = &i
		rec.Version = &v
		rec.TimeSource = m.TimeSource.String()
		rec.OriginTime = m.OriginTime.UTC().Format(originTimeLayout)
		rec.OriginUS = &us
		rec.Sequence = &seq

		rec.StreamID = hex.EncodeToString(m.StreamID[:])
		if m.Payload != nil {
			p := base64.StdEncoding.EncodeToString(m.Payload)
			rec.Payload = &p
		}

		w.write(&rec)
	}
}

type recordWriter struct {
	jsonl *bufio.Writer
	csv   *csv.Writer
	err   error
}

func newRecordWriter(format string, out io.Writer) *recordWriter {
	if format == outCSV {
		w := csv.NewWriter(out)
		_ = w.Write(csvHeader())

		return &recordWriter{csv: w}
	}

	return &recordWriter{jsonl: bufio.NewWriter(out)}
}

func (w *recordWriter) write(r *record) {
	if w.err != nil {
		return
	}

	if w.csv != nil {
		w.err = w.csv.Write(csvRow(r))

		return
	}

	line, err := json.Marshal(r)
	if err != nil {
		w.err = err

		return
	}

	line = append(line, '\n')
	_, w.err = w.jsonl.Write(line)
}

func (w *recordWriter) flush() error {
	if w.err != nil {
		return w.err
	}

	if w.csv != nil {
		w.csv.Flush()

		if err := w.csv.Error(); err != nil {
			return fmt.Errorf("seimark: write csv: %w", err)
		}

		return nil
	}

	if err := w.jsonl.Flush(); err != nil {
		return fmt.Errorf("seimark: write jsonl: %w", err)
	}

	return nil
}

// csvField renders one optional record field for a CSV row: "" when the
// pointer is nil, its formatted value otherwise.
func csvField(v any) string {
	if s, ok := v.(string); ok {
		return s
	}

	if csvFieldIsNil(v) {
		return ""
	}

	switch x := v.(type) {
	case *uint64:
		return strconv.FormatUint(*x, 10)
	case *int64:
		return strconv.FormatInt(*x, 10)
	case *uint32:
		return strconv.FormatUint(uint64(*x), 10)
	case *int:
		return strconv.Itoa(*x)
	case *bool:
		return strconv.FormatBool(*x)
	case *float64:
		return strconv.FormatFloat(*x, 'f', -1, 64)
	case *string:
		return *x
	}

	return ""
}

// csvFieldIsNil reports whether v holds a nil pointer of one of the optional
// record field types.
func csvFieldIsNil(v any) bool {
	switch x := v.(type) {
	case *uint64:
		return x == nil
	case *int64:
		return x == nil
	case *uint32:
		return x == nil
	case *int:
		return x == nil
	case *bool:
		return x == nil
	case *float64:
		return x == nil
	case *string:
		return x == nil
	}

	return false
}

func csvRow(r *record) []string {
	return []string{
		strconv.Itoa(r.AU), csvField(r.DTS), csvField(r.PTS), csvField(r.Timescale), csvField(r.Sync), csvField(r.Time),
		csvField(r.MarkerIndex), csvField(r.Version), r.TimeSource, r.OriginTime, csvField(r.OriginUS), csvField(r.Sequence),
		r.StreamID, csvField(r.Payload),
	}
}
