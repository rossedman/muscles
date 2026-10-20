// Package ui holds muscles' terminal presentation: terminal detection, the
// color theme (decoration only, never the sole carrier of meaning), the
// banner and aligned tables.
package ui

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

// IsTerminal reports whether w is a terminal.
func IsTerminal(w any) bool {
	f, ok := w.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}

// Width returns the terminal width of w, or 0 when unknown.
func Width(w any) int {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return cols
}

// ColorAllowed reports whether a stream that is a terminal may get color:
// not with --no-color, NO_COLOR or TERM=dumb.
func ColorAllowed(noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

// NewWriter wraps w so styled text is downsampled to what the terminal
// supports. Without color it returns w.
func NewWriter(w io.Writer, color bool) io.Writer {
	if !color {
		return w
	}
	cw := colorprofile.NewWriter(w, os.Environ())
	if cw.Profile <= colorprofile.NoTTY {
		cw.Profile = colorprofile.ANSI256
	}
	return cw
}

// Brand colors. The change colors match the docs site and the banner.
var (
	Cyan   = lipgloss.Color("#38bdf8")
	Violet = lipgloss.Color("#a78bfa")
	Pink   = lipgloss.Color("#f472b6")
	Green  = lipgloss.Color("#4ade80")
	Amber  = lipgloss.Color("#fbbf24")
	Red    = lipgloss.Color("#f87171")
	Gray   = lipgloss.Color("#8b93a7")
)

// Theme styles text when color is on and returns it unchanged otherwise.
type Theme struct {
	On                               bool
	Added, Modified, Meta, Deleted   lipgloss.Style
	Bold, Muted, Head, Cmd, Flag     lipgloss.Style
	OK, Warn, Err, Accent, Highlight lipgloss.Style
}

// NewTheme returns the theme; color false makes every style a no-op.
func NewTheme(color bool) *Theme {
	s := lipgloss.NewStyle()
	return &Theme{
		On:        color,
		Added:     s.Foreground(Green),
		Modified:  s.Foreground(Amber),
		Meta:      s.Foreground(Violet),
		Deleted:   s.Foreground(Red),
		Bold:      s.Bold(true),
		Muted:     s.Foreground(Gray),
		Head:      s.Foreground(Cyan).Bold(true),
		Cmd:       s.Foreground(Violet),
		Flag:      s.Foreground(Cyan),
		OK:        s.Foreground(Green),
		Warn:      s.Foreground(Amber),
		Err:       s.Foreground(Red).Bold(true),
		Accent:    s.Foreground(Pink),
		Highlight: s.Foreground(Cyan),
	}
}

// Paint renders text in style when color is on.
func (t *Theme) Paint(style lipgloss.Style, text string) string {
	if !t.On || text == "" {
		return text
	}
	return style.Render(text)
}

// Painter returns a function that paints in style.
func (t *Theme) Painter(style lipgloss.Style) func(string) string {
	return func(s string) string { return t.Paint(style, s) }
}
