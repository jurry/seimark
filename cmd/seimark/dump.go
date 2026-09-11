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

var csvHeader = []string{"au", "dts", "pts", "timescale", "sync", "time", "marker_index", "version", "time_source", "origin_time", "origin_us", "sequence", "stream_id", "payload"}

func runDump(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "auto", "input format: auto, annexb or mp4")
	out := fs.String("out", "jsonl", "output format: jsonl or csv")
	all := fs.Bool("all", false, "also print access units without a marker")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "seimark dump: exactly one FILE is required")
		return 2
	}
	if *out != "jsonl" && *out != "csv" {
		fmt.Fprintf(stderr, "seimark dump: -out must be jsonl or csv, got %q\n", *out)
		return 2
	}
	if *format != "auto" && *format != "annexb" && *format != "mp4" {
		fmt.Fprintf(stderr, "seimark dump: -format must be auto, annexb or mp4, got %q\n", *format)
		return 2
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "seimark dump: %v\n", err)
		return 1
	}
	defer f.Close()

	if *format == "auto" {
		*format, err = sniff(f)
		if err != nil {
			fmt.Fprintf(stderr, "seimark dump: %v; pass -format\n", err)
			return 2
		}
	}
	w := newRecordWriter(*out, stdout)
	warn := func(au int, err error) { fmt.Fprintf(stderr, "seimark dump: access unit %d: %v\n", au, err) }
	var walkErr error
	switch *format {
	case "annexb":
		walkErr = dumpAnnexB(f, w, *all, warn)
	case "mp4":
		walkErr = dumpMP4(f, w, *all, warn)
	}
	if err := w.flush(); err != nil {
		fmt.Fprintf(stderr, "seimark dump: write: %v\n", err)
		return 1
	}
	if walkErr != nil {
		fmt.Fprintf(stderr, "seimark dump: %v\n", walkErr)
		return 1
	}
	return 0
}

// sniff decides between mp4 and annexb from the first bytes and rewinds.
func sniff(f io.ReadSeeker) (string, error) {
	var head [12]byte
	n, _ := io.ReadFull(f, head[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if n >= 8 {
		switch string(head[4:8]) {
		case "ftyp", "moov", "moof", "styp":
			return "mp4", nil
		}
	}
	if h264.DetectFormat(head[:n]) == h264.FormatAnnexB {
		return "annexb", nil
	}
	return "", errors.New("cannot tell the input format from its first bytes")
}

func dumpAnnexB(r io.Reader, w *recordWriter, all bool, warn func(int, error)) error {
	index := 0
	for au, err := range h264.AccessUnits(r) {
		if err != nil {
			return err
		}
		emit(w, record{AU: index}, au, h264.FormatAnnexB, all, warn)
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
		emit(w, base, s.Data, h264.FormatLengthPrefixed, all, warn)
	}
	return nil
}

// emit writes one record per marker in the access unit, or the bare record when
// all is set and no marker was found.
func emit(w *recordWriter, base record, au []byte, f h264.Format, all bool, warn func(int, error)) {
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
		rec := base
		i := i
		v := marker.Version
		us := m.OriginTime.UnixMicro()
		seq := m.Sequence
		rec.MarkerIndex = &i
		rec.Version = &v
		rec.TimeSource = m.TimeSource.String()
		rec.OriginTime = m.OriginTime.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
		rec.OriginUS = &us
		rec.Sequence = &seq
		rec.StreamID = hex.EncodeToString(m.StreamID[:])
		if m.Payload != nil {
			p := base64.StdEncoding.EncodeToString(m.Payload)
			rec.Payload = &p
		}
		w.write(rec)
	}
}

type recordWriter struct {
	jsonl *bufio.Writer
	csv   *csv.Writer
	err   error
}

func newRecordWriter(format string, out io.Writer) *recordWriter {
	if format == "csv" {
		w := csv.NewWriter(out)
		_ = w.Write(csvHeader)
		return &recordWriter{csv: w}
	}
	return &recordWriter{jsonl: bufio.NewWriter(out)}
}

func (w *recordWriter) write(r record) {
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
		return w.csv.Error()
	}
	return w.jsonl.Flush()
}

func csvRow(r record) []string {
	str := func(v any) string {
		switch x := v.(type) {
		case *uint64:
			if x != nil {
				return strconv.FormatUint(*x, 10)
			}
		case *int64:
			if x != nil {
				return strconv.FormatInt(*x, 10)
			}
		case *uint32:
			if x != nil {
				return strconv.FormatUint(uint64(*x), 10)
			}
		case *int:
			if x != nil {
				return strconv.Itoa(*x)
			}
		case *bool:
			if x != nil {
				return strconv.FormatBool(*x)
			}
		case *float64:
			if x != nil {
				return strconv.FormatFloat(*x, 'f', -1, 64)
			}
		case *string:
			if x != nil {
				return *x
			}
		case string:
			return x
		}
		return ""
	}
	return []string{
		strconv.Itoa(r.AU), str(r.DTS), str(r.PTS), str(r.Timescale), str(r.Sync), str(r.Time),
		str(r.MarkerIndex), str(r.Version), r.TimeSource, r.OriginTime, str(r.OriginUS), str(r.Sequence), r.StreamID, str(r.Payload),
	}
}
