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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, ConflictPolicy: ConflictPolicyRootWins})
	if err != nil {
		t.Fatalf("ResolveLocalPackageGraph returned error: %v", err)
	}

	got := packageIDs(graph)
	want := []string{"c.pkg@1.0.0", "b.pkg@1.0.0", "a.pkg@1.0.0"}
	assertStringSliceEqual(t, got, want)

	if graph.Root == nil {
		t.Fatal("expected graph.Root to be set")
	}
	if graph.Root.Name != "a.pkg" || graph.Root.Version != "1.0.0" {
		t.Fatalf("unexpected root package %s@%s, want a.pkg@1.0.0", graph.Root.Name, graph.Root.Version)
	}
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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, ConflictPolicy: ConflictPolicyRootWins})
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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, ConflictPolicy: ConflictPolicyRootWins})
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
	downloadDir := filepath.Join(dir, ".momus", "packages")

	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"missing.pkg": "1.0.0"})

	// Missing locally and not fetchable remotely; use a controlled registry that
	// always fails so the test does not depend on the real network. The error must
	// surface the fetch failure (with context), not a stale local not-found error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldBaseURLs := packageRegistryBaseURLs
	oldClient := httpClient
	packageRegistryBaseURLs = []string{server.URL}
	httpClient = server.Client()
	defer func() {
		packageRegistryBaseURLs = oldBaseURLs
		httpClient = oldClient
	}()

	_, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, DownloadDir: downloadDir, ConflictPolicy: ConflictPolicyRootWins})
	if err == nil {
		t.Fatal("expected missing dependency error, got nil")
	}
	if !strings.Contains(err.Error(), "fetch dependency missing.pkg@1.0.0") {
		t.Fatalf("expected fetch-failure error, got %v", err)
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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, ConflictPolicy: ConflictPolicyRootWins})
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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, DownloadDir: downloadDir, ConflictPolicy: ConflictPolicyRootWins})
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

	graph, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, DownloadDir: downloadDir, ConflictPolicy: ConflictPolicyRootWins})
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

func TestIsFloatingVersion(t *testing.T) {
	floating := []string{"latest", "current", "*", " LATEST ", " * "}
	for _, v := range floating {
		if !isFloatingVersion(v) {
			t.Errorf("isFloatingVersion(%q) = false, want true", v)
		}
	}

	// A version containing "x" or "*" as part of a concrete version string is
	// not floating; only the exact tokens latest/current/* are.
	notFloating := []string{"", "1.0.0", "2.0.0x", "1.0.x", "1.0.0-beta", "v1.0.0"}
	for _, v := range notFloating {
		if isFloatingVersion(v) {
			t.Errorf("isFloatingVersion(%q) = true, want false", v)
		}
	}
}

func TestResolveRequestedVersionEmptyPrefersRootPreferred(t *testing.T) {
	selected := map[string]string{"b.pkg": "1.0.0"}
	rootPreferred := map[string]string{"b.pkg": "2.0.0"}

	got, overridden, err := resolveRequestedVersion("b.pkg", "", selected, rootPreferred, ConflictPolicyRootWins)
	if err != nil {
		t.Fatalf("resolveRequestedVersion returned error: %v", err)
	}
	if got != "2.0.0" || !overridden {
		t.Fatalf("got %q overridden=%v, want 2.0.0 overridden=true", got, overridden)
	}
}

func TestResolveRequestedVersionBranches(t *testing.T) {
	// Empty version, no root preferred, uses selected.
	got, overridden, err := resolveRequestedVersion("a.pkg", "", map[string]string{"a.pkg": "1.0.0"}, nil, ConflictPolicyRootWins)
	if err != nil || got != "1.0.0" || overridden {
		t.Fatalf("empty->selected = %q, %v, %v", got, overridden, err)
	}
	// Empty version, no selection -> empty.
	got, overridden, err = resolveRequestedVersion("a.pkg", "", map[string]string{}, nil, ConflictPolicyRootWins)
	if err != nil || got != "" || overridden {
		t.Fatalf("empty->none = %q, %v, %v", got, overridden, err)
	}
	// Floating with a selected concrete version -> uses selected.
	got, overridden, err = resolveRequestedVersion("a.pkg", "latest", map[string]string{"a.pkg": "1.0.0"}, nil, ConflictPolicyRootWins)
	if err != nil || got != "1.0.0" || !overridden {
		t.Fatalf("floating->selected = %q, %v, %v", got, overridden, err)
	}
	// Floating with root preferred -> uses it.
	got, overridden, err = resolveRequestedVersion("a.pkg", "latest", map[string]string{}, map[string]string{"a.pkg": "2.0.0"}, ConflictPolicyRootWins)
	if err != nil || got != "2.0.0" || !overridden {
		t.Fatalf("floating->root = %q, %v, %v", got, overridden, err)
	}
	// Floating, nothing -> empty overridden.
	got, overridden, err = resolveRequestedVersion("a.pkg", "latest", map[string]string{}, nil, ConflictPolicyRootWins)
	if err != nil || got != "" || !overridden {
		t.Fatalf("floating->none = %q, %v, %v", got, overridden, err)
	}
	// Exact selected match.
	got, overridden, err = resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{"a.pkg": "1.0.0"}, nil, ConflictPolicyRootWins)
	if err != nil || got != "1.0.0" || overridden {
		t.Fatalf("exact match = %q, %v, %v", got, overridden, err)
	}
	// Conflict root-wins.
	got, overridden, err = resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{"a.pkg": "2.0.0"}, nil, ConflictPolicyRootWins)
	if err != nil || got != "2.0.0" || !overridden {
		t.Fatalf("conflict root-wins = %q, %v, %v", got, overridden, err)
	}
	// Conflict strict -> error.
	if _, _, err := resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{"a.pkg": "2.0.0"}, nil, ConflictPolicyStrict); err == nil {
		t.Fatal("expected strict conflict error")
	}
	// Unsupported policy.
	if _, _, err := resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{"a.pkg": "2.0.0"}, nil, ConflictPolicy("weird")); err == nil {
		t.Fatal("expected unsupported policy error")
	}
	// Root preferred conflict.
	got, overridden, err = resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{}, map[string]string{"a.pkg": "2.0.0"}, ConflictPolicyRootWins)
	if err != nil || got != "2.0.0" || !overridden {
		t.Fatalf("root conflict = %q, %v, %v", got, overridden, err)
	}
	// New version selected.
	got, overridden, err = resolveRequestedVersion("a.pkg", "1.0.0", map[string]string{}, nil, ConflictPolicyRootWins)
	if err != nil || got != "1.0.0" || overridden {
		t.Fatalf("new version = %q, %v, %v", got, overridden, err)
	}
}

func TestFindDependencyArchivePrefersExactLocalVersion(t *testing.T) {
	index := map[string]string{
		"b.pkg@latest": "/path/b.pkg-latest.tgz",
		"b.pkg@1.0.0":  "/path/b.pkg-1.0.0.tgz",
	}

	p, err := findDependencyArchive(index, Dependency{Name: "b.pkg", Version: "latest"})
	if err != nil {
		t.Fatalf("findDependencyArchive returned error: %v", err)
	}
	if p != "/path/b.pkg-latest.tgz" {
		t.Fatalf("got %q, want exact local match /path/b.pkg-latest.tgz", p)
	}
}

func TestFindDependencyArchiveEdgeCases(t *testing.T) {
	// Empty name.
	if _, err := findDependencyArchive(map[string]string{}, Dependency{Name: ""}); err == nil {
		t.Fatal("expected error for empty name")
	}
	// Missing exact version.
	if _, err := findDependencyArchive(map[string]string{"a.pkg@2.0.0": "/x"}, Dependency{Name: "a.pkg", Version: "1.0.0"}); err == nil {
		t.Fatal("expected error for missing exact version")
	}
	// No matches.
	if _, err := findDependencyArchive(map[string]string{}, Dependency{Name: "a.pkg", Version: "latest"}); err == nil {
		t.Fatal("expected error when no matches")
	}
	// Ambiguous (multiple versions).
	index := map[string]string{
		"a.pkg@1.0.0": "/a-1.0.tgz",
		"a.pkg@2.0.0": "/a-2.0.tgz",
	}
	if _, err := findDependencyArchive(index, Dependency{Name: "a.pkg", Version: "latest"}); err == nil {
		t.Fatal("expected error for ambiguous versions")
	}
	// Single match resolves.
	index = map[string]string{"a.pkg@1.0.0": "/a-1.0.tgz"}
	p, err := findDependencyArchive(index, Dependency{Name: "a.pkg", Version: "latest"})
	if err != nil || p != "/a-1.0.tgz" {
		t.Fatalf("findDependencyArchive(single) = %q, %v", p, err)
	}
}

func TestIndexLocalPackageArchivesFileAsDir(t *testing.T) {
	// A path that is a file (not a directory) yields an empty index.
	file := filepath.Join(t.TempDir(), "afile.json")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := IndexLocalPackageArchives(file)
	if err != nil {
		t.Fatalf("IndexLocalPackageArchives(file): %v", err)
	}
	if len(idx) != 0 {
		t.Fatalf("file-as-dir index = %v, want empty", idx)
	}
}

func TestIndexLocalPackageArchives(t *testing.T) {
	// Empty dir -> error.
	if idx, err := IndexLocalPackageArchives(""); err == nil {
		t.Fatalf("expected error for empty dir, got %v", idx)
	}
	// Non-existent dir -> empty index.
	idx, err := IndexLocalPackageArchives(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("IndexLocalPackageArchives(missing): %v", err)
	}
	if len(idx) != 0 {
		t.Fatalf("missing dir index = %v, want empty", idx)
	}
	// A directory with a valid package archive.
	dir := t.TempDir()
	writePackageArchive(t, dir, "a.pkg", "1.0.0", nil)
	idx, err = IndexLocalPackageArchives(dir)
	if err != nil {
		t.Fatalf("IndexLocalPackageArchives: %v", err)
	}
	if _, ok := idx["a.pkg@1.0.0"]; !ok {
		t.Fatalf("index = %v, want a.pkg@1.0.0", idx)
	}
	// Archives without a readable manifest are skipped (not an error).
	bad := filepath.Join(dir, "bad.tgz")
	if err := os.WriteFile(bad, []byte("not-gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err = IndexLocalPackageArchives(dir)
	if err != nil {
		t.Fatalf("IndexLocalPackageArchives(with bad): %v", err)
	}
	// The valid archive is still indexed.
	if _, ok := idx["a.pkg@1.0.0"]; !ok {
		t.Fatalf("index after bad archive = %v, want a.pkg@1.0.0", idx)
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

// TestResolveLocalPackageGraphSurfacesRemoteFetchError verifies that when a
// dependency is missing locally and the remote fetch fails, the returned error
// surfaces the actual fetch failure (with context) rather than the stale local
// "not found" error from findDependencyArchive, which previously masked the
// real cause (e.g. a transient network or registry failure).
func TestResolveLocalPackageGraphSurfacesRemoteFetchError(t *testing.T) {
	dir := t.TempDir()
	downloadDir := filepath.Join(dir, ".momus", "packages")
	rootPath := writePackageArchive(t, dir, "a.pkg", "1.0.0", map[string]string{"b.pkg": "1.0.0"})

	// The registry always fails (404 on metadata) so the remote fetch cannot
	// succeed; the resolved error must reflect this, not the local lookup miss.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldBaseURLs := packageRegistryBaseURLs
	oldClient := httpClient
	packageRegistryBaseURLs = []string{server.URL}
	httpClient = server.Client()
	defer func() {
		packageRegistryBaseURLs = oldBaseURLs
		httpClient = oldClient
	}()

	_, err := ResolveLocalPackageGraphWithOptions(rootPath, ResolveOptions{DepsDir: dir, DownloadDir: downloadDir, ConflictPolicy: ConflictPolicyRootWins})
	if err == nil {
		t.Fatal("expected a dependency resolution error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "fetch dependency b.pkg@1.0.0") {
		t.Fatalf("error does not surface the fetch failure with context: %v", err)
	}
	if !strings.Contains(msg, "unexpected status 404") {
		t.Fatalf("error does not surface the underlying fetch cause: %v", err)
	}
	if strings.Contains(msg, "dependency archive not found for b.pkg@1.0.0") {
		t.Fatalf("error still masks the real fetch cause with the local not-found error: %v", err)
	}
}
