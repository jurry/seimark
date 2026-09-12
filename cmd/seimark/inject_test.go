package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jurry/seimark/h264"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "vectors", "streams", name)
}

// strippedFixture writes the Annex B fixture without its markers to a temp file.
func strippedFixture(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(fixturePath("testsrc-marked.h264"))
	if err != nil {
		t.Fatal(err)
	}

	var out []byte

	for au, err := range h264.AccessUnits(bytes.NewReader(data)) {
		if err != nil {
			t.Fatal(err)
		}

		s, err := h264.StripMarkers(au, h264.FormatAnnexB)
		if err != nil {
			t.Fatal(err)
		}

		out = append(out, s...)
	}

	path := filepath.Join(t.TempDir(), "plain.h264")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func dumpLines(t *testing.T, path string) []string {
	t.Helper()

	var out, errOut bytes.Buffer
	if code := run([]string{"dump", path}, &out, &errOut); code != 0 {
		t.Fatalf("dump exit %d: %s", code, errOut.String())
	}

	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

func TestInjectMarksEveryUnitWithRateFromSPS(t *testing.T) {
	t.Parallel()

	in := strippedFixture(t)
	out := filepath.Join(t.TempDir(), "marked.h264")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", "-start", "2026-09-12T21:00:00Z", "-stream-id", "9f3c1a77e2b04d51", in, out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	lines := dumpLines(t, out)
	if len(lines) != 20 {
		t.Fatalf("%d records, want 20", len(lines))
	}

	if !strings.Contains(lines[0], `"sequence":0`) || !strings.Contains(lines[0], `"origin_time":"2026-09-12T21:00:00.000000Z"`) {
		t.Fatalf("first record: %s", lines[0])
	}

	if !strings.Contains(lines[1], `"origin_time":"2026-09-12T21:00:00.100000Z"`) {
		t.Fatalf("second record at 10 fps: %s", lines[1])
	}

	if !strings.Contains(lines[19], `"sequence":19`) || strings.Contains(lines[0], `"payload"`) {
		t.Fatalf("last record: %s", lines[19])
	}
}

func TestInjectFPSOverridesSPS(t *testing.T) {
	t.Parallel()

	in := strippedFixture(t)
	out := filepath.Join(t.TempDir(), "marked.h264")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", "-start", "2026-09-12T21:00:00Z", "-fps", "5", in, out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	lines := dumpLines(t, out)
	if !strings.Contains(lines[1], `"origin_time":"2026-09-12T21:00:00.200000Z"`) {
		t.Fatalf("second record at 5 fps: %s", lines[1])
	}
}

func TestInjectKeyframesOnly(t *testing.T) {
	t.Parallel()

	in := strippedFixture(t)
	out := filepath.Join(t.TempDir(), "marked.h264")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", "-start", "2026-09-12T21:00:00Z", "-keyframes-only", in, out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}

	lines := dumpLines(t, out)
	if len(lines) != 2 || !strings.Contains(lines[1], `"au":10`) || !strings.Contains(lines[1], `"sequence":1`) {
		t.Fatalf("keyframes-only records: %v", lines)
	}
}

func TestInjectRefusesMarkedInput(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "marked.h264")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", fixturePath("testsrc-marked.h264"), out}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "already") {
		t.Fatalf("stderr: %s", stderr.String())
	}
}

func TestInjectNoRateIsAUsageError(t *testing.T) {
	t.Parallel()

	// SPS bytes from h264's tests are not a parsable SPS, so there is no rate to read.
	in := filepath.Join(t.TempDir(), "norate.h264")

	stream := append([]byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x0d, 0xda, 0x05, 0x82, 0x51, 0, 0, 0, 1, 0x68, 0xce, 0x38, 0x80}, 0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00, 0x33, 0xff)
	if err := os.WriteFile(in, stream, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", in, filepath.Join(t.TempDir(), "out.h264")}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2; stderr: %s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "-fps") {
		t.Fatalf("stderr should ask for -fps: %s", stderr.String())
	}

	if code := run([]string{"inject", "-fps", "25", in, filepath.Join(t.TempDir(), "out.h264")}, &stdout, &stderr); code != 0 {
		t.Fatalf("with -fps: exit %d: %s", code, stderr.String())
	}
}

func TestInjectRefusesMP4(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"inject", fixturePath("testsrc-marked.mp4"), filepath.Join(t.TempDir(), "out")}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2; stderr: %s", code, stderr.String())
	}
}

func TestInjectUsageErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	if code := run([]string{"inject", "only-one-arg"}, &stdout, &stderr); code != 2 {
		t.Fatalf("one arg: exit %d", code)
	}

	if code := run([]string{"inject", "-stream-id", "zz", "a", "b"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad stream id: exit %d", code)
	}

	if code := run([]string{"inject", "-placement", "middle", "a", "b"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad placement: exit %d", code)
	}
}
