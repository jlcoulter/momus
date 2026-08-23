package mock

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
)

func TestServerRespondsWithFixedStatusAndBody(t *testing.T) {
	s := New(http.StatusTeapot, "teapot")
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	resp, err := http.Get("http://" + addr + "/anything")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "teapot" {
		t.Fatalf("body = %q, want %q", body, "teapot")
	}
}

func TestServerEmptyBody(t *testing.T) {
	s := New(http.StatusNoContent, "")
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestWithBasePathOption(t *testing.T) {
	s := New(http.StatusOK, "ok", WithBasePath("/fhir/"))
	if s.basePath != "/fhir" {
		t.Fatalf("WithBasePath did not trim trailing slash: got %q, want %q", s.basePath, "/fhir")
	}

	// With the base path set, a request under /fhir is routed to the plan
	// handler with the prefix stripped. Use plan-aware mode so routing occurs.
	plan := New(http.StatusOK, "ok", WithBasePath("/fhir"), WithPlanAware())
	addr, err := plan.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer plan.Close()

	resp, err := http.Get("http://" + addr + "/fhir/metadata")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestWithLoggerOption(t *testing.T) {
	s := New(http.StatusOK, "ok", WithLogger(false))
	if s.logger {
		t.Fatal("WithLogger(false) did not disable logging")
	}
	s2 := New(http.StatusOK, "ok", WithLogger(true))
	if !s2.logger {
		t.Fatal("WithLogger(true) did not enable logging")
	}
}

func TestServerWithPort(t *testing.T) {
	// Pick a free port, then release it so the server can bind to it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	s := New(http.StatusOK, "ok", WithPort(port))
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	want := fmt.Sprintf("127.0.0.1:%d", port)
	if addr != want {
		t.Fatalf("addr = %q, want %q", addr, want)
	}
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
