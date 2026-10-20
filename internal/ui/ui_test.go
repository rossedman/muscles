package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTerminal(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) || Width(&bytes.Buffer{}) != 0 {
		t.Fatal("buffer is not a terminal")
	}
	f, _ := os.CreateTemp(t.TempDir(), "x")
	defer f.Close()
	if IsTerminal(f) || Width(f) != 0 {
		t.Fatal("file is not a terminal")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	if !ColorAllowed(false) || ColorAllowed(true) {
		t.Fatal("ColorAllowed")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorAllowed(false) {
		t.Fatal("NO_COLOR ignored")
	}
	var b bytes.Buffer
	if NewWriter(&b, false) != &b {
		t.Fatal("NewWriter without color")
	}
	w := NewWriter(&b, true)
	w.Write([]byte(NewTheme(true).Paint(NewTheme(true).OK, "ok")))
	if !strings.Contains(b.String(), "ok") || !strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("colored writer: %q", b.String())
	}
}

func TestBanner(t *testing.T) {
	plain := Banner(NewTheme(false), "v1.0.0")
	if !strings.Contains(plain, "███╗   ███╗") || !strings.Contains(plain, Tagline+" · v1.0.0") || strings.Contains(plain, "\x1b") {
		t.Fatalf("plain banner:\n%s", plain)
	}
	colored := Banner(NewTheme(true), "v1.0.0")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("colored banner has no color")
	}
	if c := gradient(-1); c == nil {
		t.Fatal("gradient")
	}
	if a, b := gradient(2), gradient(1); a != b {
		t.Fatal("gradient clamp")
	}
}

func TestTable(t *testing.T) {
	th := NewTheme(false)
	tb := Table{Columns: []Column{{Title: "#", Right: true}, {Title: "SIZE", Right: true}, {Title: "STEP", Max: 10}, {Title: "NOTE"}}}
	tb.Add(C("0"), Cell{Text: "74 MB", Plain: "74000000"}, C("FROM debian:bookworm-slim"), C(""))
	tb.Add(C("12"), Cell{Text: "1 KB", Paint: th.Painter(th.OK)}, C("RUN a|b"), C("⚠ note"))
	var b bytes.Buffer
	if err := tb.Render(&b, th); err != nil {
		t.Fatal(err)
	}
	want := " #   SIZE  STEP        NOTE\n 0  74 MB  FROM debi…\n12   1 KB  RUN a|b     ⚠ note\n"
	if b.String() != want {
		t.Fatalf("render:\n%q\nwant\n%q", b.String(), want)
	}
	b.Reset()
	tb.Plain(&b)
	if b.String() != "0\t74000000\tFROM debian:bookworm-slim\t\n12\t1 KB\tRUN a|b\t⚠ note\n" {
		t.Fatalf("plain: %q", b.String())
	}
	b.Reset()
	tb.Rows[0][2] = C("/usr/lib")
	tb.Markdown(&b)
	if !strings.Contains(b.String(), "| # | SIZE | STEP | NOTE |\n| ---: | ---: | --- | --- |\n| 0 | 74 MB | `/usr/lib` |  |\n| 12 | 1 KB | RUN a\\|b | ⚠ note |") {
		t.Fatalf("markdown:\n%s", b.String())
	}
	// Color is applied after padding.
	b.Reset()
	ct := NewTheme(true)
	tb.Rows[1][1].Paint = ct.Painter(ct.OK)
	tb.Render(&b, ct)
	if !strings.Contains(b.String(), "\x1b[") {
		t.Fatal("no color in colored table")
	}
}
