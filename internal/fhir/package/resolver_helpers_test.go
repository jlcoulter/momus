package fhirpackage

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVersionFromMetadata(t *testing.T) {
	// Nil metadata.
	if _, _, err := resolveVersionFromMetadata(Dependency{Name: "a", Version: "1.0.0"}, nil); err == nil {
		t.Fatal("expected error for nil metadata")
	}
	meta := &registryPackageMetadata{
		DistTags: map[string]string{"latest": "2.0.0"},
		Versions: map[string]registryPackageVersionMeta{
			"1.0.0": {Version: "1.0.0", Dist: struct {
				Tarball string `json:"tarball"`
			}{Tarball: "http://x/1.0.0.tgz"}},
			"2.0.0": {Version: "2.0.0", Dist: struct {
				Tarball string `json:"tarball"`
			}{Tarball: "http://x/2.0.0.tgz"}},
		},
	}
	// Exact version.
	ver, tarball, err := resolveVersionFromMetadata(Dependency{Version: "1.0.0"}, meta)
	if err != nil || ver != "1.0.0" || tarball != "http://x/1.0.0.tgz" {
		t.Fatalf("exact = %q, %q, %v", ver, tarball, err)
	}
	// Floating "latest" resolves via dist-tags.
	ver, _, err = resolveVersionFromMetadata(Dependency{Version: "latest"}, meta)
	if err != nil || ver != "2.0.0" {
		t.Fatalf("latest = %q, %v", ver, err)
	}
	// Version not in metadata.
	if _, _, err := resolveVersionFromMetadata(Dependency{Version: "3.0.0"}, meta); err == nil {
		t.Fatal("expected error for missing version")
	}
	// Version with missing tarball.
	meta2 := &registryPackageVersionMeta{}
	metaNoTarball := &registryPackageMetadata{Versions: map[string]registryPackageVersionMeta{"1.0.0": *meta2}}
	if _, _, err := resolveVersionFromMetadata(Dependency{Version: "1.0.0"}, metaNoTarball); err == nil {
		t.Fatal("expected error for missing tarball")
	}
}

func TestDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "file.tgz")
	if err := downloadFile(server.URL+"/file", dest); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "content" {
		t.Fatalf("downloaded content = %q, %v", data, err)
	}
	// Non-2xx status.
	if err := downloadFile(server.URL+"/missing", filepath.Join(t.TempDir(), "x.tgz")); err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	// Invalid URL.
	if err := downloadFile("://invalid", filepath.Join(t.TempDir(), "y.tgz")); err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

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
