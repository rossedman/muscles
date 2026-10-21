package dockerfile

import (
	"strings"
	"testing"
)

// steps is the history TestLines matches against df, one step per line.
const steps = "ADD file:4f2a in /\nRUN apt-get update && apt-get install -y curl\nWORKDIR /app\nRUN pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*\nCOPY dir:9a8b7c in /app/\nCOPY . .\nRUN python -m compileall /app\nCOPY /out/app /usr/local/bin/app\n"

// FuzzParse feeds arbitrary Dockerfiles and histories to Parse and Lines:
// --dockerfile is user input, so it may be rejected but must never crash,
// and what is parsed must keep the line and stage invariants Lines relies on.
func FuzzParse(f *testing.F) {
	f.Add(df, steps)
	f.Add("FROM alpine\nRUN <<A <<'B'\na\nA\nb\nB\nCOPY x /y\n", "RUN <<A <<'B'\nCOPY file:1 in /y")
	f.Add("from a\nrun echo \\\n\n  hi\n# c\nFROM b AS c\nADD [\"a\", \"/b\"]\n", "ADD a /b\n\nRUN echo hi")
	f.Add("\\\n\\", "\\")
	f.Fuzz(func(t *testing.T, src, history string) {
		ins, err := Parse(strings.NewReader(src))
		if err != nil {
			return
		}
		nLines := strings.Count(src, "\n") + 1
		for i, in := range ins {
			if in.Cmd == "" || in.Line < 1 || in.Line > nLines || in.Stage < 0 {
				t.Fatalf("instruction %d: %+v", i, in)
			}
			if i > 0 && (in.Line <= ins[i-1].Line || in.Stage < ins[i-1].Stage) {
				t.Fatalf("instruction %d out of order: %+v after %+v", i, in, ins[i-1])
			}
		}
		final := Final(ins)
		lineOf := map[int]bool{}
		for _, in := range final {
			if in.Cmd == "FROM" {
				t.Fatalf("Final kept a FROM: %+v", in)
			}
			lineOf[in.Line] = true
		}

		st := strings.Split(history, "\n")
		lines := Lines(st, ins)
		if len(lines) != len(st) {
			t.Fatalf("%d lines for %d steps", len(lines), len(st))
		}
		last := 0
		for i, l := range lines {
			if l == 0 {
				continue
			}
			if !lineOf[l] {
				t.Fatalf("step %d %q matched line %d, not a final-stage instruction", i, st[i], l)
			}
			if l <= last {
				t.Fatalf("step %d matched line %d after line %d: matches must keep the Dockerfile's order", i, l, last)
			}
			if strings.TrimSpace(st[i]) == "" {
				t.Fatalf("blank step %d matched line %d", i, l)
			}
			last = l
		}
	})
}

// BenchmarkParse measures reading a multi-stage Dockerfile with
// continuations, comments and heredocs.
func BenchmarkParse(b *testing.B) {
	src := strings.Repeat(df, 20)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(strings.NewReader(src)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLines measures matching an image's history to the final stage,
// as images inspect --dockerfile does for every layer.
func BenchmarkLines(b *testing.B) {
	ins, err := Parse(strings.NewReader(df))
	if err != nil {
		b.Fatal(err)
	}
	st := strings.Split(strings.Repeat(steps, 4), "\n")
	b.ReportAllocs()
	for b.Loop() {
		if got := Lines(st, ins); len(got) != len(st) {
			b.Fatalf("%d lines for %d steps", len(got), len(st))
		}
	}
}
