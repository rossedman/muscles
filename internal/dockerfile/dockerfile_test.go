package dockerfile

import (
	"errors"
	"strings"
	"testing"
)

const df = `# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
RUN go build -o /out/app .

from debian:bookworm-slim
ENV PYTHONUNBUFFERED=1
RUN apt-get update \
    && apt-get install -y curl
# a comment between
WORKDIR /app
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*
COPY --chown=app:app . .
RUN <<EOF
echo one
echo two
EOF
RUN ["python", "-m", "compileall", "/app"]
COPY --from=build /out/app /usr/local/bin/app
CMD ["python", "main.py"]
`

func TestParse(t *testing.T) {
	ins, err := Parse(strings.NewReader(df))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 13 {
		t.Fatalf("%d instructions: %+v", len(ins), ins)
	}
	run := ins[5]
	if run.Line != 8 || run.Cmd != "RUN" || run.Args != "apt-get update && apt-get install -y curl" || run.Stage != 1 {
		t.Fatalf("continued RUN: %+v", run)
	}
	if h := ins[9]; h.Line != 15 || h.Cmd != "RUN" || ins[10].Line != 19 {
		t.Fatalf("heredoc: %+v then %+v", h, ins[10])
	}
	final := Final(ins)
	if len(final) != 9 || final[0].Cmd != "ENV" {
		t.Fatalf("final: %+v", final)
	}
	if _, err := Parse(errReader{}); err == nil {
		t.Fatal("read error swallowed")
	}
	if Final(nil) != nil {
		t.Fatal("Final(nil)")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestLines(t *testing.T) {
	ins, _ := Parse(strings.NewReader(df))
	steps := []string{
		"ADD file:4f2a in /", // base image
		"RUN apt-get update && apt-get install -y curl",
		"WORKDIR /app",
		"RUN pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*",
		"COPY dir:9a8b7c in /app/", // a layer the Dockerfile has no line for
		"COPY . .",
		"RUN python -m compileall /app",
		"COPY /out/app /usr/local/bin/app",
		"",
	}
	got := Lines(steps, ins)
	want := []int{0, 8, 11, 12, 0, 14, 19, 20, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d %q: line %d, want %d", i, steps[i], got[i], want[i])
		}
	}
	// The legacy builder's COPY matches by destination.
	legacy := Lines([]string{"COPY file:abc in /usr/local/bin/app"}, ins)
	if legacy[0] != 20 {
		t.Errorf("legacy COPY: %v", legacy)
	}
	if dest("") != "" || dest("dir:x in /a/") != "/a/" {
		t.Error("dest")
	}
	// Nothing to match against.
	if l := Lines([]string{"RUN x"}, nil); l[0] != 0 {
		t.Error("no instructions")
	}
}
