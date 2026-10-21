// Package dockerfile reads a Dockerfile's instructions with their line
// numbers and matches them to an image's build steps, so a layer can point
// at the line that made it. It does no I/O beyond the reader it is given.
package dockerfile

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// Instruction is one Dockerfile instruction.
type Instruction struct {
	Line  int    // 1-based line of the instruction's first line
	Cmd   string // upper case: RUN, COPY, ...
	Args  string // the rest, continuation lines joined
	Stage int    // 0-based build stage (each FROM starts one)
}

var heredoc = regexp.MustCompile(`<<-?["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)

// Parse reads every instruction. Comments, blank lines, continuation lines
// (`\`) and heredoc bodies are folded into the instruction they belong to.
func Parse(r io.Reader) ([]Instruction, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var (
		out   []Instruction
		cur   *Instruction
		parts []string
		stage = -1
		lineN = 0
		until []string // heredoc terminators still to read
	)
	finish := func() {
		if cur == nil {
			return
		}
		cur.Args = strings.TrimSpace(strings.Join(parts, " "))
		out = append(out, *cur)
		cur, parts = nil, nil
	}
	for sc.Scan() {
		lineN++
		line := sc.Text()
		if len(until) > 0 {
			if strings.TrimSpace(line) == until[0] {
				until = until[1:]
				if len(until) == 0 {
					finish()
				}
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || (trimmed == "" && cur == nil) {
			continue
		}
		if cur == nil {
			cmd, rest, _ := strings.Cut(trimmed, " ")
			cmd = strings.ToUpper(cmd)
			if cmd == "FROM" {
				stage++
			}
			cur = &Instruction{Line: lineN, Cmd: cmd, Stage: max(stage, 0)}
			trimmed = rest
		}
		cont := strings.HasSuffix(trimmed, `\`)
		parts = append(parts, strings.TrimSpace(strings.TrimSuffix(trimmed, `\`)))
		if cont {
			continue
		}
		for _, m := range heredoc.FindAllStringSubmatch(strings.Join(parts, " "), -1) {
			until = append(until, m[1])
		}
		if len(until) == 0 {
			finish()
		}
	}
	finish()
	return out, sc.Err()
}

// Final returns the instructions of the last build stage: the one the
// image is made from.
func Final(ins []Instruction) []Instruction {
	last := 0
	for _, in := range ins {
		last = max(last, in.Stage)
	}
	var out []Instruction
	for _, in := range ins {
		if in.Stage == last && in.Cmd != "FROM" {
			out = append(out, in)
		}
	}
	return out
}

// normalize turns a step or an instruction into comparable text: the
// command, its flags (--mount, --chown, ...) dropped, an exec-form array
// joined, whitespace collapsed.
func normalize(cmd, args string) string {
	fields := strings.Fields(args)
	for len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
		fields = fields[1:]
	}
	rest := strings.Join(fields, " ")
	if strings.HasPrefix(rest, "[") {
		var words []string
		if json.Unmarshal([]byte(rest), &words) == nil {
			rest = strings.Join(words, " ")
		}
	}
	return strings.ToUpper(cmd) + " " + rest
}

// dest is where a COPY or ADD puts its files: its last argument, or, for
// the legacy builder's "COPY dir:abc123 in /app", what follows " in ".
func dest(args string) string {
	if i := strings.LastIndex(args, " in "); i >= 0 && (strings.HasPrefix(args, "file:") || strings.HasPrefix(args, "dir:") || strings.HasPrefix(args, "multi:")) {
		return strings.TrimSpace(args[i+4:])
	}
	f := strings.Fields(args)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// split separates a step into its instruction and arguments.
func split(step string) (string, string) {
	cmd, args, _ := strings.Cut(strings.TrimSpace(step), " ")
	return cmd, args
}

// Lines matches steps (as analysis.CleanStep writes them, oldest first) to
// the final stage's instructions and returns each step's line, 0 where
// none matches. Matching runs from the newest step back, in order, so base
// image layers before the first instruction stay unmatched.
func Lines(steps []string, ins []Instruction) []int {
	final := Final(ins)
	lines := make([]int, len(steps))
	j := len(final) - 1
	for i := len(steps) - 1; i >= 0 && j >= 0; i-- {
		cmd, args := split(steps[i])
		if cmd == "" {
			continue
		}
		want := normalize(cmd, args)
		found := -1
		for k := j; k >= 0; k-- {
			if normalize(final[k].Cmd, final[k].Args) == want {
				found = k
				break
			}
		}
		if found < 0 && (strings.EqualFold(cmd, "COPY") || strings.EqualFold(cmd, "ADD")) {
			for k := j; k >= 0; k-- {
				if strings.EqualFold(final[k].Cmd, cmd) && dest(final[k].Args) == dest(args) {
					found = k
					break
				}
			}
		}
		if found >= 0 {
			lines[i] = final[found].Line
			j = found - 1
		}
	}
	return lines
}
