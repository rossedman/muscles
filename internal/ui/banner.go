package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// logo is "muscles" in the ANSI Shadow figlet font.
var logo = []string{
	"███╗   ███╗██╗   ██╗███████╗ ██████╗██╗     ███████╗███████╗",
	"████╗ ████║██║   ██║██╔════╝██╔════╝██║     ██╔════╝██╔════╝",
	"██╔████╔██║██║   ██║███████╗██║     ██║     █████╗  ███████╗",
	"██║╚██╔╝██║██║   ██║╚════██║██║     ██║     ██╔══╝  ╚════██║",
	"██║ ╚═╝ ██║╚██████╔╝███████║╚██████╗███████╗███████╗███████║",
	"╚═╝     ╚═╝ ╚═════╝ ╚══════╝ ╚═════╝╚══════╝╚══════╝╚══════╝",
}

// Tagline is the one-line description under the banner.
const Tagline = "explain container images, layer by layer"

// stops is the banner's left-to-right gradient.
var stops = [][3]float64{{0x38, 0xbd, 0xf8}, {0xa7, 0x8b, 0xfa}, {0xf4, 0x72, 0xb6}}

func gradient(t float64) color.Color {
	if t <= 0 {
		t = 0
	}
	if t >= 1 {
		t = 1
	}
	seg := t * float64(len(stops)-1)
	i := int(seg)
	if i >= len(stops)-1 {
		i = len(stops) - 2
	}
	f := seg - float64(i)
	a, b := stops[i], stops[i+1]
	mix := func(x, y float64) uint8 { return uint8(x + (y-x)*f + 0.5) }
	return color.RGBA{R: mix(a[0], b[0]), G: mix(a[1], b[1]), B: mix(a[2], b[2]), A: 0xff}
}

// Banner is the logo with a gradient, a strip of layer colors and the
// tagline. Without color it is the plain logo, which still reads.
func Banner(t *Theme, version string) string {
	var b strings.Builder
	width := len([]rune(logo[0]))
	for row, line := range logo {
		b.WriteString("  ")
		runes := []rune(line)
		for col, r := range runes {
			ch := string(r)
			if !t.On || r == ' ' {
				b.WriteString(ch)
				continue
			}
			c := gradient(float64(col)/float64(width-1) + float64(row)*0.015)
			b.WriteString(lipgloss.NewStyle().Foreground(c).Render(ch))
		}
		b.WriteString("\n")
	}
	// A stack of layers in the change colors: added, modified, meta, deleted.
	b.WriteString("  ")
	segments := []struct {
		style lipgloss.Style
		n     int
	}{{t.Added, 22}, {t.Modified, 14}, {t.Meta, 14}, {t.Deleted, 10}}
	for _, s := range segments {
		b.WriteString(t.Paint(s.style, strings.Repeat("▀", s.n)))
	}
	b.WriteString("\n")
	tag := fmt.Sprintf("  %s %s", t.Paint(t.Bold, Tagline), t.Paint(t.Muted, "· "+version))
	b.WriteString(tag + "\n")
	return b.String()
}
