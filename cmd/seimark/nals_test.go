package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNalsAnnexBFixture(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if code := run([]string{"nals", fixturePath("testsrc-marked.h264")}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	text := out.String()

	if n := strings.Count(text, "\nau ") + 1; n != 20 {
		t.Fatalf("%d access units, want 20", n)
	}

	if n := strings.Count(text, "seimark seq="); n != 20 {
		t.Fatalf("%d seimark lines, want 20", n)
	}

	if !strings.Contains(text, "seq=0 time=2026-09-12T21:00:00.000000Z stream=9f3c1a77e2b04d51 payload=7") {
		t.Fatalf("first marker line missing:\n%s", text)
	}

	if !strings.Contains(text, "user_data_unregistered uuid=") {
		t.Fatalf("x264 user data not listed as foreign:\n%s", text)
	}

	if !strings.HasPrefix(text, "au 0\n") {
		t.Fatalf("output starts with %q", text[:20])
	}
}

func TestNalsMP4Fixture(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	if code := run([]string{"nals", fixturePath("testsrc-marked.mp4")}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	text := out.String()
	if !strings.HasPrefix(text, "au 0 dts 0 pts 0\n") || strings.Count(text, "seimark seq=") != 20 {
		t.Fatalf("unexpected output:\n%s", text[:200])
	}
}

func TestNalsErrors(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer

	if code := run([]string{"nals"}, &out, &errOut); code != 2 {
		t.Fatalf("no file: exit %d", code)
	}

	if code := run([]string{"nals", "/etc/hostname"}, &out, &errOut); code != 2 {
		t.Fatalf("unrecognisable input: exit %d", code)
	}
}
