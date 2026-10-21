package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rossedman/muscles/internal/ui"
)

// status is the one live progress line on stderr. It only draws when
// stderr is a terminal; elsewhere progress is silent (or debug lines with
// --verbose), so logs never fill with redraws.
type status struct {
	w     io.Writer
	th    *ui.Theme
	live  bool
	quiet bool

	mu    sync.Mutex
	text  string
	shown bool
	stop  chan struct{}
	frame int
}

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// start shows text and animates the spinner until clear.
func (s *status) start(text string) {
	if !s.live || s.quiet {
		return
	}
	s.mu.Lock()
	s.text = text
	if s.stop == nil {
		s.stop = make(chan struct{})
		go s.spin(s.stop)
	}
	s.drawLocked()
	s.mu.Unlock()
}

// set changes the text, starting the line if needed.
func (s *status) set(text string) { s.start(text) }

// spin redraws the spinner every 100ms until stop is closed.
func (s *status) spin(stop chan struct{}) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.mu.Lock()
			s.frame++
			s.drawLocked()
			s.mu.Unlock()
		}
	}
}

// drawLocked draws the spinner frame and text on the current line. The caller holds s.mu.
func (s *status) drawLocked() {
	if s.text == "" {
		return
	}
	fmt.Fprintf(s.w, "\r\x1b[K%s %s", s.th.Paint(s.th.Highlight, frames[s.frame%len(frames)]), s.th.Paint(s.th.Muted, s.text))
	s.shown = true
}

// clear stops the spinner and erases the line.
func (s *status) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
	if s.shown {
		fmt.Fprint(s.w, "\r\x1b[K")
		s.shown = false
	}
	s.text = ""
}

// jsonEncoder returns an encoder that writes indented JSON without escaping HTML.
func jsonEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc
}
