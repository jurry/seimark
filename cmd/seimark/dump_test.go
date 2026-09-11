package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDumpGolden(t *testing.T) {
	for _, name := range []string{"testsrc-marked.h264", "testsrc-marked.mp4", "testsrc-marked-frag.mp4"} {
		t.Run(name, func(t *testing.T) {
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
}
