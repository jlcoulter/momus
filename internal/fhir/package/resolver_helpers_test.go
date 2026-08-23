package fhirpackage

import (
	"net/http"
	"testing"
)

func TestPackageKey(t *testing.T) {
	if got := packageKey("a.pkg", "1.0.0"); got != "a.pkg@1.0.0" {
		t.Fatalf("packageKey = %q", got)
	}
}

func TestIsPackageArchivePath(t *testing.T) {
	if !isPackageArchivePath("x.tgz") || !isPackageArchivePath("x.tar.gz") {
		t.Fatal("archive paths should be recognized")
	}
	if !isPackageArchivePath("x.TGZ") {
		t.Fatal("archive paths should be case-insensitive")
	}
	if isPackageArchivePath("x.json") {
		t.Fatal("non-archive path should not be recognized")
	}
}

func TestFloatingVersionTag(t *testing.T) {
	if got := floatingVersionTag("current"); got != "latest" {
		t.Fatalf("floatingVersionTag(current) = %q", got)
	}
	if got := floatingVersionTag("latest"); got != "latest" {
		t.Fatalf("floatingVersionTag(latest) = %q", got)
	}
	if got := floatingVersionTag("*"); got != "*" {
		t.Fatalf("floatingVersionTag(*) = %q", got)
	}
}

func TestSanitizeFileComponent(t *testing.T) {
	if got := sanitizeFileComponent("a/b:c d"); got != "a_b_c_d" {
		t.Fatalf("sanitizeFileComponent = %q", got)
	}
}

func TestSortedDependencies(t *testing.T) {
	deps := []Dependency{
		{Name: "b", Version: "2.0"},
		{Name: "a", Version: "1.0"},
		{Name: "a", Version: "1.0"}, // duplicate
		{Name: "", Version: "x"},    // empty name dropped
	}
	out := sortedDependencies(deps)
	if len(out) != 2 {
		t.Fatalf("sortedDependencies = %v", out)
	}
	if out[0].Name != "a" || out[0].Version != "1.0" || out[1].Name != "b" {
		t.Fatalf("sortedDependencies order = %v", out)
	}
	if sortedDependencies(nil) != nil {
		t.Fatal("sortedDependencies(nil) should be nil")
	}
}

func TestApplyRegistryAuth(t *testing.T) {
	applyRegistryAuth(nil) // no panic

	registryAuth = registryHTTPAuth{}
	req, _ := http.NewRequest("GET", "http://x", nil)
	applyRegistryAuth(req)
	if req.Header.Get("Authorization") != "" {
		t.Fatal("no auth should set nothing")
	}

	registryAuth = registryHTTPAuth{BearerToken: "tok"}
	req, _ = http.NewRequest("GET", "http://x", nil)
	applyRegistryAuth(req)
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("bearer auth = %q", req.Header.Get("Authorization"))
	}

	registryAuth = registryHTTPAuth{BasicUsername: "u", BasicPassword: "p"}
	req, _ = http.NewRequest("GET", "http://x", nil)
	applyRegistryAuth(req)
	if _, _, ok := req.BasicAuth(); !ok {
		t.Fatal("expected basic auth")
	}
}
