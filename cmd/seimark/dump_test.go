package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDumpGolden(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"testsrc-marked.h264", "testsrc-marked.mp4", "testsrc-marked-frag.mp4"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("..", "..", "vectors", "streams", name)

			want, err := os.ReadFile(path + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}

			var out, errOut bytes.Buffer

			code := run([]string{"dump", "-out", "jsonl", path}, &out, &errOut)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, errOut.String())
			}

			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("output differs from %s.jsonl:\n%s", name, out.String())
			}
		})
	}
}

func TestDumpCSVHeaderAndCount(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "vectors", "streams", "testsrc-marked.mp4")

	var out, errOut bytes.Buffer
	if code := run([]string{"dump", "-out", "csv", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	lines := bytes.Split(bytes.TrimRight(out.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 21 {
		t.Fatalf("%d lines, want header + 20", len(lines))
	}

	if !bytes.HasPrefix(lines[0], []byte("au,dts,pts,timescale,sync,time,marker_index,version,time_source,origin_time,origin_us,sequence,stream_id,payload")) {
		t.Fatalf("header: %s", lines[0])
	}
}

func TestDumpAllIncludesUnmarked(t *testing.T) {
	t.Parallel()
	// The h264 fixture has a marker in every access unit, so -all changes nothing there;
	// build a two-unit stream with one unmarked unit instead.
	dir := t.TempDir()
	path := filepath.Join(dir, "two.h264")

	marked, err := os.ReadFile(filepath.Join("..", "..", "vectors", "streams", "testsrc-marked.h264"))
	if err != nil {
		t.Fatal(err)
	}
	// An unmarked IDR access unit appended after the fixture: SPS, PPS, IDR slice.
	unmarked := []byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x0d, 0xda, 0x05, 0x82, 0x51, 0, 0, 0, 1, 0x68, 0xce, 0x38, 0x80, 0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00, 0x33, 0xff}
	if err := os.WriteFile(path, append(marked, unmarked...), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"dump", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	if n := bytes.Count(out.Bytes(), []byte("\n")); n != 20 {
		t.Fatalf("%d records without -all, want 20", n)
	}

	out.Reset()

	if code := run([]string{"dump", "-all", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	if n := bytes.Count(out.Bytes(), []byte("\n")); n != 21 {
		t.Fatalf("%d records with -all, want 21", n)
	}

	if !bytes.Contains(out.Bytes(), []byte(`{"au":20}`)) {
		t.Fatalf("unmarked record missing or has marker fields:\n%s", out.String())
	}
}

func TestDumpUsageErrors(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if code := run([]string{"dump"}, &out, &errOut); code != 2 {
		t.Fatalf("no file: exit %d, want 2", code)
	}

	if code := run([]string{"dump", "-out", "xml", "x"}, &out, &errOut); code != 2 {
		t.Fatalf("bad -out: exit %d, want 2", code)
	}

	if code := run([]string{"nope"}, &out, &errOut); code != 2 {
		t.Fatalf("unknown command: exit %d, want 2", code)
	}

	if code := run([]string{"dump", filepath.Join(t.TempDir(), "missing.h264")}, &out, &errOut); code != 1 {
		t.Fatalf("missing file: exit %d, want 1", code)
	}

	unknown := filepath.Join(t.TempDir(), "unknown.bin")
	if err := os.WriteFile(unknown, []byte("not a stream and not an mp4"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"dump", unknown}, &out, &errOut); code != 2 {
		t.Fatalf("unrecognised format: exit %d, want 2", code)
	}
}

func TestDumpDirectoryIsAnIOError(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer

	code := run([]string{"dump", t.TempDir()}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr: %s", code, errOut.String())
	}

	if !strings.Contains(errOut.String(), "is a directory") {
		t.Fatalf("stderr = %q, want it to mention the directory", errOut.String())
	}
}

func TestDumpHelpExitsZero(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if code := run([]string{"dump", "-h"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, want 0; stderr: %s", code, errOut.String())
	}
}

func TestDumpWarnsOnUnparsableSEIAndKeepsGoing(t *testing.T) {
	t.Parallel()
	// An SEI NAL unit that mp4ff cannot parse, then a normal fixture access
	// unit: the marker records are still written and the exit code stays 0.
	path := filepath.Join("..", "..", "vectors", "streams", "testsrc-marked.h264")

	stream, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	spoiled := filepath.Join(t.TempDir(), "spoiled.h264")
	garbage := make([]byte, 0, 8+len(stream))

	garbage = append(garbage, 0, 0, 0, 1, 0x06, 0xff, 0xff, 0xff)
	if err := os.WriteFile(spoiled, append(garbage, stream...), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"dump", "-format", "annexb", spoiled}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, want 0; stderr: %s", code, errOut.String())
	}

	if n := bytes.Count(out.Bytes(), []byte("\n")); n != 20 {
		t.Fatalf("%d records, want 20", n)
	}

	if !strings.Contains(errOut.String(), "does not parse") {
		t.Fatalf("stderr = %q, want a warning about the unparsable SEI", errOut.String())
	}
}
