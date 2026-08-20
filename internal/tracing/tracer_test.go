package tracing

import (
	"bytes"
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

	tr.LogRequest(req, []byte(`{"resourceType":"Patient"}`))
	tr.LogResponse(req, http.StatusCreated, http.Header{"ETag": []string{`W/"1"`}}, []byte(`{"id":"1"}`))

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
	tr.LogResponse(req, http.StatusOK, nil, []byte("0123456789abcdef"))

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
			tr.LogRequest(req, nil)
			tr.LogResponse(req, http.StatusOK, nil, nil)
		})
	}
	wg.Wait()

	if got := strings.Count(buf.String(), "==> REQUEST #"); got != 50 {
		t.Errorf("expected 50 request traces, got %d", got)
	}
}

func TestTracerColorOffByDefaultForNonTerminal(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf) // bytes.Buffer is not a terminal, so colour stays off

	req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	tr.LogRequest(req, nil)
	tr.LogResponse(req, http.StatusOK, nil, nil)

	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("expected no ANSI codes for a non-terminal writer:\n%q", buf.String())
	}
}

func TestTracerColorWhenForced(t *testing.T) {
	var buf bytes.Buffer
	tr := New(&buf)
	tr.color = true

	req, _ := http.NewRequest(http.MethodGet, "http://host/fhir/Patient", nil)
	tr.LogRequest(req, nil)
	tr.LogResponse(req, http.StatusCreated, nil, nil)

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
