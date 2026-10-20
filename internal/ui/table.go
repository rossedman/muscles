package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Column is a table column.
type Column struct {
	Title string
	Right bool // right-align (numbers)
	Max   int  // truncate wider cells with "…" in the aligned view; 0: no limit
}

// Cell is one value; Paint styles it after alignment, so color never
// shifts columns. Plain is the value for line-oriented output when it
// should differ from Text (for example, an exact byte count).
type Cell struct {
	Text  string
	Plain string
	Paint func(string) string
}

// C returns an unstyled cell.
func C(text string) Cell { return Cell{Text: text} }

// Table is rows of cells under columns.
type Table struct {
	Columns []Column
	Rows    [][]Cell
}

// Add appends a row.
func (t *Table) Add(cells ...Cell) { t.Rows = append(t.Rows, cells) }

// Render writes the table aligned for a terminal, with a header row.
func (t *Table) Render(w io.Writer, th *Theme) error {
	widths := make([]int, len(t.Columns))
	for i, c := range t.Columns {
		widths[i] = ansi.StringWidth(c.Title)
	}
	cell := func(i int, c Cell) string {
		s := c.Text
		if m := t.Columns[i].Max; m > 0 && ansi.StringWidth(s) > m {
			s = ansi.Truncate(s, m-1, "") + "…"
		}
		return s
	}
	for _, r := range t.Rows {
		for i, c := range r {
			if n := ansi.StringWidth(cell(i, c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var b strings.Builder
	line := func(texts []string, paints []func(string) string) {
		var parts []string
		for i, s := range texts {
			pad := strings.Repeat(" ", widths[i]-ansi.StringWidth(s))
			last := i == len(texts)-1
			if paints != nil && paints[i] != nil {
				s = paints[i](s)
			}
			switch {
			case t.Columns[i].Right:
				s = pad + s
			case !last:
				s += pad
			}
			parts = append(parts, s)
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, "  "), " ") + "\n")
	}
	heads := make([]string, len(t.Columns))
	hp := make([]func(string) string, len(t.Columns))
	for i, c := range t.Columns {
		heads[i], hp[i] = c.Title, th.Painter(th.Muted)
	}
	line(heads, hp)
	for _, r := range t.Rows {
		texts := make([]string, len(r))
		paints := make([]func(string) string, len(r))
		for i, c := range r {
			texts[i], paints[i] = cell(i, c), c.Paint
		}
		line(texts, paints)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Plain writes one tab-separated record per line, no header and no
// styling, for cut, sort and grep.
func (t *Table) Plain(w io.Writer) error {
	var b strings.Builder
	for _, r := range t.Rows {
		vals := make([]string, len(r))
		for i, c := range r {
			v := c.Text
			if c.Plain != "" {
				v = c.Plain
			}
			vals[i] = strings.ReplaceAll(v, "\t", " ")
		}
		b.WriteString(strings.Join(vals, "\t") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Markdown writes a GitHub-flavored Markdown table.
func (t *Table) Markdown(w io.Writer) error {
	var b strings.Builder
	esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }
	var heads, rules []string
	for _, c := range t.Columns {
		heads = append(heads, esc(c.Title))
		if c.Right {
			rules = append(rules, "---:")
		} else {
			rules = append(rules, "---")
		}
	}
	fmt.Fprintf(&b, "| %s |\n| %s |\n", strings.Join(heads, " | "), strings.Join(rules, " | "))
	for _, r := range t.Rows {
		vals := make([]string, len(r))
		for i, c := range r {
			vals[i] = esc(c.Text)
			if vals[i] != "" && !t.Columns[i].Right && i > 0 && strings.HasPrefix(c.Text, "/") {
				vals[i] = "`" + c.Text + "`"
			}
		}
		fmt.Fprintf(&b, "| %s |\n", strings.Join(vals, " | "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
