package fhirpackage

import (
	"archive/tar"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocalPackageGraphLinear(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})
	writePackageArchive(t, dir, "b.pkg", "1.0.0", map[string]string{"c.pkg": "1.0.0"})
	writePackageArchive(t, dir, "c.pkg", "1.0.0", nil)

	graph, err := ResolveLocalPackageGraph(rootPath, dir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"c.pkg@1.0.0", "b.pkg@1.0.0", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)
}

func TestResolveLocalPackageGraphDiamondDeDupes(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{
		"b.pkg": "1.0.0",
		"c.pkg": "1.0.0",
	})
	writePackageArchive(t, dir, "b.pkg", "1.0.0", map[string]string{"d.pkg": "1.0.0"})
	writePackageArchive(t, dir, "c.pkg", "1.0.0", map[string]string{"d.pkg": "1.0.0"})
	writePackageArchive(t, dir, "d.pkg", "1.0.0", nil)

	graph, err := ResolveLocalPackageGraph(rootPath, dir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	if len(graph.Packages) != 4 {
		t.Fatalf("got %d resolved packages, want 4", len(graph.Packages))
	}

	ids := packageIDs(graph)
	if countOccurrences(ids, "d.pkg@1.0.0") != 1 {
		t.Fatalf("expected d.pkg@1.0.0 once, got %v", ids)
	}
}

func TestResolveLocalPackageGraphCycle(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})
	writePackageArchive(t, dir, "b.pkg", "1.0.0", map[string]string{"a.pkg": "1.0.0"})

	graph, err := ResolveLocalPackageGraph(rootPath, dir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"b.pkg@1.0.0", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)
	if countOccurrences(got, "a.pkg@1.0.0") != 1 || countOccurrences(got, "b.pkg@1.0.0") != 1 {
		t.Fatalf("expected cyclic packages to be resolved once each, got %v", got)
	}
}

func TestResolveLocalPackageGraphMissingDependency(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"missing.pkg": "1.0.0"})

	_, err := ResolveLocalPackageGraph(rootPath, dir)
	if err == nil {
		t.Fatal("expected missing dependency error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestResolveLocalPackageGraphVersionConflict(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{
		"b.pkg": "1.0.0",
		"c.pkg": "1.0.0",
	})
	writePackageArchive(t, dir, "b.pkg", "1.0.0", nil)
	writePackageArchive(t, dir, "c.pkg", "1.0.0", map[string]string{"b.pkg": "2.0.0"})
	writePackageArchive(t, dir, "b.pkg", "2.0.0", nil)

	_, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{
		DepsDir:        dir,
		ConflictPolicy: ConflictPolicyStrict,
	})
	if err == nil {
		t.Fatal("expected version conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("expected version conflict error, got %v", err)
	}
}

func TestResolveLocalPackageGraphRootWinsConflictPolicy(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{
		"b.pkg": "2.0.0",
		"c.pkg": "1.0.0",
	})
	writePackageArchive(t, dir, "b.pkg", "2.0.0", nil)
	writePackageArchive(t, dir, "b.pkg", "1.0.0", nil)
	writePackageArchive(t, dir, "c.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})

	graph, err := ResolveLocalPackageGraph(rootPath, dir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"b.pkg@2.0.0", "c.pkg@1.0.0", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)
	if countOccurrences(got, "b.pkg@1.0.0") != 0 {
		t.Fatalf("expected root-wins to exclude b.pkg@1.0.0, got %v", got)
	}
}

func TestResolveLocalPackageGraphStrictConflictPolicy(t *testing.T) {
	dir := t.TempDir()

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{
		"b.pkg": "2.0.0",
		"c.pkg": "1.0.0",
	})
	writePackageArchive(t, dir, "b.pkg", "2.0.0", nil)
	writePackageArchive(t, dir, "b.pkg", "1.0.0", nil)
	writePackageArchive(t, dir, "c.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})

	_, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{
		DepsDir:        dir,
		ConflictPolicy: ConflictPolicyStrict,
	})
	if err == nil {
		t.Fatal("expected strict conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("expected version conflict error, got %v", err)
	}
}

func TestResolveLocalPackageGraphFetchesMissingDependencyRemotely(t *testing.T) {
	dir := t.TempDir()
	downloadDir := filepath.Join(dir, ".momus", "packages")
	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})
	remoteArchive := buildTestPackageArchiveAtPath(t, filepath.Join(t.TempDir(), "b.pkg-1.0.0.tgz"), map[string]any{
		"package/package.json": map[string]any{
			"name":         "b.pkg",
			"version":      "1.0.0",
			"dependencies": map[string]string{},
		},
		"package/ValueSet-b.pkg.json": map[string]any{
			"resourceType": "ValueSet",
			"url":          "http://example.org/ValueSet/b.pkg",
			"version":      "1.0.0",
			"name":         "b.pkgValueSet",
			"status":       "active",
		},
	})
	remoteBytes, err := os.ReadFile(remoteArchive)
	if err != nil {
		t.Fatalf("failed to read remote archive fixture: %v", err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b.pkg":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(mustMarshalJSON(t, map[string]any{
				"dist-tags": map[string]any{"latest": "1.0.0"},
				"versions": map[string]any{
					"1.0.0": map[string]any{
						"version": "1.0.0",
						"dist": map[string]any{
							"tarball": serverURL + "/tarballs/b.pkg-1.0.0.tgz",
						},
					},
				},
			}))
		case "/tarballs/b.pkg-1.0.0.tgz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(remoteBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	oldBaseURLs := packageRegistryBaseURLs
	oldClient := httpClient
	packageRegistryBaseURLs = []string{server.URL}
	httpClient = server.Client()
	defer func() {
		packageRegistryBaseURLs = oldBaseURLs
		httpClient = oldClient
	}()

	graph, err := ResolveLocalPackageGraphWithDownloadDir(rootPath, dir, downloadDir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"b.pkg@1.0.0", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)

	if _, err := os.Stat(filepath.Join(downloadDir, "b.pkg-1.0.0.tgz")); err != nil {
		t.Fatalf("expected downloaded dependency archive in download dir: %v", err)
	}
}

func TestResolveLocalPackageGraphResolvesCurrentVersionRemotely(t *testing.T) {
	dir := t.TempDir()
	downloadDir := filepath.Join(dir, ".momus", "packages")
	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"b.pkg": "current"})
	remoteArchive := buildTestPackageArchiveAtPath(t, filepath.Join(t.TempDir(), "b.pkg-2.0.1.tgz"), map[string]any{
		"package/package.json": map[string]any{
			"name":         "b.pkg",
			"version":      "2.0.1",
			"dependencies": map[string]string{},
		},
		"package/ValueSet-b.pkg.json": map[string]any{
			"resourceType": "ValueSet",
			"url":          "http://example.org/ValueSet/b.pkg",
			"version":      "2.0.1",
			"name":         "b.pkgValueSet",
			"status":       "active",
		},
	})
	remoteBytes, err := os.ReadFile(remoteArchive)
	if err != nil {
		t.Fatalf("failed to read remote archive fixture: %v", err)
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b.pkg":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(mustMarshalJSON(t, map[string]any{
				"dist-tags": map[string]any{"latest": "2.0.1"},
				"versions": map[string]any{
					"2.0.1": map[string]any{
						"version": "2.0.1",
						"dist": map[string]any{
							"tarball": serverURL + "/tarballs/b.pkg-2.0.1.tgz",
						},
					},
				},
			}))
		case "/tarballs/b.pkg-2.0.1.tgz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(remoteBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	oldBaseURLs := packageRegistryBaseURLs
	oldClient := httpClient
	packageRegistryBaseURLs = []string{server.URL}
	httpClient = server.Client()
	defer func() {
		packageRegistryBaseURLs = oldBaseURLs
		httpClient = oldClient
	}()

	graph, err := ResolveLocalPackageGraphWithDownloadDir(rootPath, dir, downloadDir)
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"b.pkg@2.0.1", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)

	if _, err := os.Stat(filepath.Join(downloadDir, "b.pkg-2.0.1.tgz")); err != nil {
		t.Fatalf("expected downloaded dependency archive in download dir: %v", err)
	}
}

func writePackageArchive(t *testing.T, dir, name, version string, deps map[string]string) string {
	t.Helper()
	archivePath := filepath.Join(dir, name+"-"+version+".tgz")

	files := map[string]any{
		"package/package.json": map[string]any{
			"name":         name,
			"version":      version,
			"dependencies": deps,
		},
		"package/ValueSet-" + name + ".json": map[string]any{
			"resourceType": "ValueSet",
			"url":          "http://example.org/ValueSet/" + name,
			"version":      version,
			"name":         name + "ValueSet",
			"status":       "active",
		},
	}

	return buildTestPackageArchiveAtPath(t, archivePath, files)
}

func buildTestPackageArchiveAtPath(t *testing.T, archivePath string, files map[string]any) string {
	t.Helper()
	rawFiles := make(map[string][]byte, len(files))
	for name, content := range files {
		rawFiles[name] = mustMarshalJSON(t, content)
	}
	return buildTestPackageArchiveWithRawFilesAtPath(t, archivePath, rawFiles)
}

func buildTestPackageArchiveWithRawFilesAtPath(t *testing.T, archivePath string, files map[string][]byte) string {
	t.Helper()

	archiveDir := filepath.Dir(archivePath)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("failed to create archive dir: %v", err)
	}

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("failed to write tar content for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return archivePath
}

func packageIDs(graph *ResolvedGraph) []string {
	out := make([]string, 0, len(graph.Packages))
	for _, pkg := range graph.Packages {
		out = append(out, pkg.Name+"@"+pkg.Version)
	}
	return out
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d want %d, got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mismatch at %d: got %q want %q (got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

func countOccurrences(values []string, target string) int {
	count := 0
	for _, v := range values {
		if v == target {
			count++
		}
	}
	return count
}
