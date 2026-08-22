package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// progressBar renders a single-line ANSI progress bar to stderr. It is used to
// give the user live feedback during long test runs (e.g. 25k cases). It only
// renders when stderr is a terminal, so piped output stays clean.
type progressBar struct {
	mu      sync.Mutex
	width   int
	started bool
}

// newProgressBar returns a progress bar that renders to stderr. width is the
// number of characters in the bar itself.
func newProgressBar(width int) *progressBar {
	return &progressBar{width: width}
}

// enabled reports whether the bar should render (stderr is a terminal).
func (p *progressBar) enabled() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// render draws the bar at the current done/total position, overwriting the
// previous line. It is a no-op when stderr is not a terminal.
func (p *progressBar) render(done, total int) {
	if !p.enabled() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if total <= 0 {
		return
	}
	pct := float64(done) / float64(total)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(p.width))
	if filled > p.width {
		filled = p.width
	}
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", p.width-filled)
	// \r returns to line start; the trailing spaces clear any leftover chars.
	fmt.Fprintf(os.Stderr, "\r  %3.0f%% [%s] %d/%d", pct*100, bar, done, total)
	p.started = true
}

// finish clears the progress line and prints a newline so subsequent output
// starts on a fresh line. It is a no-op when stderr is not a terminal.
func (p *progressBar) finish() {
	if !p.enabled() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", p.width+20))
		p.started = false
	}
}
