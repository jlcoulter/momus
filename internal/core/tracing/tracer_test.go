package tracing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTracerLogsRequestAndResponse(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)

	req, err := http.NewRequest(http.MethodPut, "http://host/fhir/Patient/1", strings.NewReader(`{"resourceType":"Patient"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Authorization", "Bearer secret-token")

	seq := tr.LogRequest(req, []byte(`{"resourceType":"Patient"}`))
	tr.LogResponse(req, seq, http.StatusCreated, http.Header{"ETag": []string{`W/"1"`}}, []byte(`{"id":"1"}`))

	out := buf.String()
	for _, want := range []string{
		"==> REQUEST #1",
		"PUT http://host/fhir/Patient/1",
		"Content-Type: application/fhir+json",
		"Authorization: <redacted>",
		`{"resourceType":"Patient"}`,
		"<== RESPONSE #1",
		"201 Created",
		`W/"1"`,
		`{"id":"1"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trace output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-token") {
		t.Errorf("trace output leaked the bearer token:\n%s", out)
	}
}

func TestTracerTruncatesLargeBodies(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)
	tr.maxBody = 8

	req, err := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	tr.LogResponse(req, 1, http.StatusOK, nil, []byte("0123456789abcdef"))

	out := buf.String()
	if !strings.Contains(out, "01234567") {
		t.Errorf("expected truncated body prefix in output:\n%s", out)
	}
	if !strings.Contains(out, "8 more bytes truncated") {
		t.Errorf("expected truncation notice in output:\n%s", out)
	}
	if strings.Contains(out, "abcdef") {
		t.Errorf("truncated body should not include the tail:\n%s", out)
	}
}

func TestTracerIsConcurrencySafe(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
			seq := tr.LogRequest(req, nil)
			tr.LogResponse(req, seq, http.StatusOK, nil, nil)
		})
	}
	wg.Wait()

	if got := strings.Count(buf.String(), "==> REQUEST #"); got != 50 {
		t.Errorf("expected 50 request traces, got %d", got)
	}
}

// TestTracerPairsResponseWithRequestSeq forces an interleaving where a second
// request is logged between a first request and its response. The response must
// be tagged with the sequence returned by its own LogRequest, not the current
// tracer sequence (which would be the second request's number).
func TestTracerPairsResponseWithRequestSeq(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)

	reqA, _ := http.NewRequest(http.MethodGet, "http://host/a", nil)
	reqB, _ := http.NewRequest(http.MethodGet, "http://host/b", nil)

	aRequested := make(chan int)
	aProceed := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		seqA := tr.LogRequest(reqA, nil)
		aRequested <- seqA
		<-aProceed
		tr.LogResponse(reqA, seqA, http.StatusOK, nil, nil)
	}()

	seqA := <-aRequested
	seqB := tr.LogRequest(reqB, nil)
	close(aProceed)
	wg.Wait()

	out := buf.String()
	if !strings.Contains(out, fmt.Sprintf("<== RESPONSE #%d", seqA)) {
		t.Errorf("expected response for request A tagged #%d:\n%s", seqA, out)
	}
	if strings.Contains(out, fmt.Sprintf("<== RESPONSE #%d", seqB)) {
		t.Errorf("response for request A was tagged with request B's sequence #%d:\n%s", seqB, out)
	}
}

func TestTracerColorOffByDefaultForNonTerminal(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf) // bytes.Buffer is not a terminal, so colour stays off

	req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	seq := tr.LogRequest(req, nil)
	tr.LogResponse(req, seq, http.StatusOK, nil, nil)

	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("expected no ANSI codes for a non-terminal writer:\n%q", buf.String())
	}
}

func TestTracerJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	tr := NewJSON(&buf)

	req, _ := http.NewRequest(http.MethodPut, "http://host/fhir/Patient/1", strings.NewReader(`{"resourceType":"Patient"}`))
	req.Header.Set("Content-Type", "application/fhir+json")
	req.Header.Set("Authorization", "Bearer secret-token")
	seq := tr.LogRequest(req, []byte(`{"resourceType":"Patient"}`))
	tr.LogResponse(req, seq, http.StatusCreated, http.Header{"ETag": []string{`W/"1"`}}, []byte(`{"id":"1"}`))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(lines), buf.String())
	}

	var reqEv, respEv Event
	if err := json.Unmarshal([]byte(lines[0]), &reqEv); err != nil {
		t.Fatalf("decode request line: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &respEv); err != nil {
		t.Fatalf("decode response line: %v", err)
	}

	if reqEv.Kind != "request" || reqEv.Sequence != 1 || reqEv.Method != http.MethodPut || reqEv.URL != "http://host/fhir/Patient/1" {
		t.Errorf("request event = %+v", reqEv)
	}
	if reqEv.Headers["Authorization"] != "<redacted>" {
		t.Errorf("expected Authorization redacted, got %q", reqEv.Headers["Authorization"])
	}
	if reqEv.Body != `{"resourceType":"Patient"}` {
		t.Errorf("request body = %q", reqEv.Body)
	}

	if respEv.Kind != "response" || respEv.Sequence != 1 || respEv.Status != http.StatusCreated {
		t.Errorf("response event = %+v", respEv)
	}
	if respEv.Headers["ETag"] != `W/"1"` {
		t.Errorf("response ETag = %q", respEv.Headers["ETag"])
	}
	if respEv.Body != `{"id":"1"}` {
		t.Errorf("response body = %q", respEv.Body)
	}
	if strings.Contains(buf.String(), "secret-token") {
		t.Errorf("JSON output leaked the bearer token:\n%s", buf.String())
	}
}

func TestTracerJSONTruncatesLargeBodies(t *testing.T) {
	var buf bytes.Buffer
	tr := NewJSON(&buf)
	tr.maxBody = 8

	req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	tr.LogResponse(req, 1, http.StatusOK, nil, []byte("0123456789abcdef"))

	var ev Event
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &ev); err != nil {
		t.Fatalf("decode line: %v", err)
	}
	if ev.Body != "01234567" {
		t.Errorf("truncated body = %q, want prefix", ev.Body)
	}
	if ev.Truncated != 8 {
		t.Errorf("truncated = %d, want 8", ev.Truncated)
	}
}

func TestTracerColorWhenForced(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)
	tr.color = true

	req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	seq := tr.LogRequest(req, nil)
	tr.LogResponse(req, seq, http.StatusCreated, nil, nil)

	out := buf.String()
	if !strings.Contains(out, "\x1b[36m") {
		t.Errorf("expected cyan request header when colour forced:\n%q", out)
	}
	if !strings.Contains(out, "\x1b[32m") {
		t.Errorf("expected green response header for 2xx when colour forced:\n%q", out)
	}
}

func TestStatusColor(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusOK, green},
		{http.StatusCreated, green},
		{http.StatusMovedPermanently, yellow},
		{http.StatusBadRequest, red},
		{http.StatusInternalServerError, red},
		{http.StatusContinue, ""},
	}
	for _, c := range cases {
		if got := statusColor(c.status); got != c.want {
			t.Errorf("statusColor(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}
