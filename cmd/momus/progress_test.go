package main

import (
	"testing"
)

func TestProgressBarRender(t *testing.T) {
	p := newProgressBar(10)
	// render is a no-op when stderr is not a terminal (as in tests), so it
	// must not panic and must not error.
	p.render(0, 100)
	p.render(50, 100)
	p.render(100, 100)
	p.finish()
}

func TestProgressBarRenderEdgeCases(t *testing.T) {
	p := newProgressBar(10)
	// total <= 0 is a no-op.
	p.render(5, 0)
	// done > total is clamped.
	p.render(150, 100)
	p.finish()
}
