// Package tracing provides a lightweight, concurrency-safe HTTP request/response
// tracer used to surface every request a Momus run makes (capability fetch,
// dataset provisioning, and test execution) when --debug is enabled.
package tracing

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

// maxBodyBytes caps how much of a request/response body is printed so a single
// large payload cannot flood debug output. The full body is still available in
// the structured report when IncludeDebug is set.
const maxBodyBytes = 4096

// ANSI escape codes used to colour trace output. They are only emitted when the
// tracer is writing to a terminal (or colour is forced on), so piped output
// stays plain.
const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	cyan   = "\x1b[36m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
)

// Tracer logs outgoing HTTP requests and their responses to a writer. It is
// safe for concurrent use (parallel test branches log from multiple goroutines).
type Tracer struct {
	mu      sync.Mutex
	w       io.Writer
	maxBody int
	seq     int
	color   bool
}

// New returns a Tracer that writes to w. When w is nil the tracer is a no-op.
// Colour is enabled automatically when w is a terminal; use SetColor to override.
func New(w io.Writer) *Tracer {
	return &Tracer{w: w, maxBody: maxBodyBytes, color: isTerminal(w)}
}

// SetColor forces colour output on or off, overriding terminal auto-detection.
func (t *Tracer) SetColor(enabled bool) {
	if t == nil {
		return
	}
	t.color = enabled
}

// LogRequest records an outgoing request. body is the request payload (may be
// nil for requests without a body).
func (t *Tracer) LogRequest(req *http.Request, body []byte) {
	if t == nil || t.w == nil || req == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	t.writeHeader(fmt.Sprintf("==> REQUEST #%d", t.seq), cyan)
	t.writeLine("%s %s", t.paint(bold, req.Method), req.URL.String())
	t.writeHeaders(req.Header)
	t.writeBody(body)
	t.writeBlank()
}

// LogResponse records the response to a request. headers and body may be nil/empty.
func (t *Tracer) LogResponse(req *http.Request, status int, headers http.Header, body []byte) {
	if t == nil || t.w == nil || req == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeHeader(fmt.Sprintf("<== RESPONSE #%d", t.seq), statusColor(status))
	t.writeLine("%s %s -> %s", req.Method, req.URL.String(), t.paint(statusColor(status), fmt.Sprintf("%d %s", status, http.StatusText(status))))
	t.writeHeaders(headers)
	t.writeBody(body)
	t.writeBlank()
}

// writeHeader prints a section title, coloured when enabled.
func (t *Tracer) writeHeader(title string, color string) {
	fmt.Fprintf(t.w, "--- %s ---\n", t.paint(color, title))
}

func (t *Tracer) writeLine(format string, args ...any) {
	fmt.Fprintf(t.w, format+"\n", args...)
}

func (t *Tracer) writeBlank() {
	fmt.Fprintln(t.w)
}

// paint wraps s in an ANSI colour code when colour is enabled, returning s
// unchanged otherwise.
func (t *Tracer) paint(code, s string) string {
	if !t.color || code == "" {
		return s
	}
	return code + s + reset
}

// writeHeaders prints headers in sorted order, redacting sensitive values.
func (t *Tracer) writeHeaders(headers http.Header) {
	if len(headers) == 0 {
		return
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		values := headers[k]
		if isSensitiveHeader(k) {
			t.writeLine("%s: %s", t.paint(dim, k), t.paint(red, "<redacted>"))
			continue
		}
		for _, v := range values {
			t.writeLine("%s: %s", t.paint(dim, k), v)
		}
	}
}

// isSensitiveHeader reports whether a header value should be redacted in trace
// output (credentials and tokens must never be echoed to the console).
func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
		return true
	}
	return false
}

// statusColor returns the colour used for a response based on its status class:
// green for 2xx, yellow for 3xx, red for 4xx/5xx, and no colour otherwise.
func statusColor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return green
	case status >= 300 && status < 400:
		return yellow
	case status >= 400:
		return red
	}
	return ""
}

func (t *Tracer) writeBody(body []byte) {
	if len(body) == 0 {
		return
	}
	original := len(body)
	if original > t.maxBody {
		body = body[:t.maxBody]
	}
	t.w.Write(body)
	if original > t.maxBody {
		fmt.Fprintf(t.w, "\n... (%d more bytes truncated)\n", original-t.maxBody)
	}
}

// isTerminal reports whether w is a character device (i.e. an interactive
// terminal) so colour can be enabled only when it will render, not when output
// is redirected to a file or pipe.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
