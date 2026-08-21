package fhirpackage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

func TestReadPackageLoadsManifestAndResources(t *testing.T) {
	archivePath := buildTestPackageArchive(t, map[string]any{
		"package/package.json": map[string]any{
			"name":    "example.fhir.pkg",
			"version": "1.2.3",
			"dependencies": map[string]string{
				"hl7.fhir.r4.core": "4.0.1",
			},
		},
		"package/StructureDefinition-test.json": map[string]any{
			"resourceType":   "StructureDefinition",
			"url":            "http://example.org/StructureDefinition/test",
			"version":        "1.0.0",
			"name":           "TestStructureDefinition",
			"type":           "Observation",
			"baseDefinition": "http://hl7.org/fhir/StructureDefinition/Observation",
			"kind":           "resource",
			"derivation":     "constraint",
			"snapshot": map[string]any{
				"element": []map[string]any{
					{"id": "Observation", "path": "Observation", "min": 0, "max": "*"},
				},
			},
		},
		"package/ValueSet-test.json": map[string]any{
			"resourceType": "ValueSet",
			"url":          "http://example.org/ValueSet/test",
			"version":      "1.0.0",
			"name":         "TestValueSet",
			"status":       "active",
		},
		"package/openapi/some.openapi.json": map[string]any{
			"openapi": "3.0.0",
			"info": map[string]any{
				"title": "Not FHIR",
			},
		},
	})

	pkg, err := ReadPackage(archivePath)
	if err != nil {
		t.Fatalf("ReadPackage returned error: %v", err)
	}

	if pkg.Name != "example.fhir.pkg" {
		t.Fatalf("got package name %q, want %q", pkg.Name, "example.fhir.pkg")
	}
	if pkg.Version != "1.2.3" {
		t.Fatalf("got package version %q, want %q", pkg.Version, "1.2.3")
	}
	if len(pkg.Dependencies) != 1 {
		t.Fatalf("got %d dependencies, want 1", len(pkg.Dependencies))
	}
	if len(pkg.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(pkg.Resources))
	}

	if _, ok := pkg.Resources[0].(*model.StructureDefinition); !ok {
		if _, ok := pkg.Resources[1].(*model.StructureDefinition); !ok {
			t.Fatalf("expected at least one StructureDefinition resource")
		}
	}
}

func TestReadPackageDecodesElementExamples(t *testing.T) {
	archivePath := buildTestPackageArchive(t, map[string]any{
		"package/package.json": map[string]any{
			"name":    "example.fhir.pkg",
			"version": "1.2.3",
		},
		"package/StructureDefinition-test.json": map[string]any{
			"resourceType":   "StructureDefinition",
			"url":            "http://example.org/StructureDefinition/test",
			"version":        "1.0.0",
			"name":           "TestStructureDefinition",
			"type":           "Observation",
			"baseDefinition": "http://hl7.org/fhir/StructureDefinition/Observation",
			"kind":           "resource",
			"derivation":     "constraint",
			"snapshot": map[string]any{
				"element": []map[string]any{
					{"id": "Observation", "path": "Observation", "min": 0, "max": "*"},
					{"id": "Observation.valueString", "path": "Observation.valueString", "min": 0, "max": "1", "type": []map[string]any{{"code": "string"}}, "example": []map[string]any{{"label": "General", "valueString": "hello"}}},
				},
			},
		},
	})

	pkg, err := ReadPackage(archivePath)
	if err != nil {
		t.Fatalf("ReadPackage returned error: %v", err)
	}
	var sd *model.StructureDefinition
	for _, res := range pkg.Resources {
		if candidate, ok := res.(*model.StructureDefinition); ok {
			sd = candidate
			break
		}
	}
	if sd == nil {
		t.Fatal("expected structure definition resource")
	}
	if len(sd.Elements) < 2 {
		t.Fatalf("got %d elements, want at least 2", len(sd.Elements))
	}
	if len(sd.Elements[1].Examples) != 1 || sd.Elements[1].Examples[0] != "hello" {
		t.Fatalf("got examples %+v, want [hello]", sd.Elements[1].Examples)
	}
}

func TestReadPackageDecodesElementBaseMax(t *testing.T) {
	archivePath := buildTestPackageArchive(t, map[string]any{
		"package/package.json": map[string]any{
			"name":    "example.fhir.pkg",
			"version": "1.2.3",
		},
		"package/StructureDefinition-test.json": map[string]any{
			"resourceType": "StructureDefinition",
			"url":          "http://example.org/StructureDefinition/test-base-max",
			"version":      "1.0.0",
			"name":         "TestBaseMax",
			"type":         "Organization",
			"snapshot": map[string]any{
				"element": []map[string]any{
					{"id": "Organization", "path": "Organization", "min": 0, "max": "*"},
					{"id": "Organization.address", "path": "Organization.address", "min": 1, "max": "1", "base": map[string]any{"path": "Organization.address", "min": 0, "max": "*"}},
				},
			},
		},
	})

	pkg, err := ReadPackage(archivePath)
	if err != nil {
		t.Fatalf("ReadPackage returned error: %v", err)
	}
	var sd *model.StructureDefinition
	for _, res := range pkg.Resources {
		if candidate, ok := res.(*model.StructureDefinition); ok {
			sd = candidate
			break
		}
	}
	if sd == nil {
		t.Fatal("expected structure definition resource")
	}
	if len(sd.Elements) < 2 {
		t.Fatalf("got %d elements, want at least 2", len(sd.Elements))
	}
	if sd.Elements[1].BaseMax != "*" {
		t.Fatalf("got base max %q, want *", sd.Elements[1].BaseMax)
	}
}

func TestReadPackageHandlesBOMManifest(t *testing.T) {
	archivePath := buildTestPackageArchiveWithRawFiles(t, map[string][]byte{
		"package/package.json": withUTF8BOM(mustMarshalJSON(t, map[string]any{
			"name":    "bom.pkg",
			"version": "9.9.9",
		})),
		"package/ValueSet-test.json": mustMarshalJSON(t, map[string]any{
			"resourceType": "ValueSet",
			"url":          "http://example.org/ValueSet/test",
			"version":      "1.0.0",
			"name":         "TestValueSet",
			"status":       "active",
		}),
	})

	pkg, err := ReadPackage(archivePath)
	if err != nil {
		t.Fatalf("ReadPackage returned error: %v", err)
	}

	if pkg.Name != "bom.pkg" {
		t.Fatalf("got package name %q, want %q", pkg.Name, "bom.pkg")
	}
	if pkg.Version != "9.9.9" {
		t.Fatalf("got package version %q, want %q", pkg.Version, "9.9.9")
	}
}

func TestRegistryBuilderBuildRoutesSupportedResourceTypes(t *testing.T) {
	builder := NewRegistryBuilder()
	pkg := &Package{
		Name:    "route-test",
		Version: "1.0.0",
		Resources: []any{
			&model.StructureDefinition{URL: "http://example.org/StructureDefinition/a", Type: "Observation"},
			&model.ValueSet{URL: "http://example.org/ValueSet/a"},
			&model.CodeSystem{URL: "http://example.org/CodeSystem/a"},
			&model.SearchParameter{Code: "code", Base: []string{"Observation"}},
			&model.CapabilityStatement{URL: "http://example.org/CapabilityStatement/a"},
			map[string]any{"resourceType": "Unknown"},
		},
	}

	r, err := builder.BuildFromPackages([]*Package{pkg})
	if err != nil {
		t.Fatalf("BuildFromPackages returned error: %v", err)
	}

	if _, ok := r.StructureDefinition("http://example.org/StructureDefinition/a"); !ok {
		t.Fatal("expected StructureDefinition to be indexed")
	}
	if _, ok := r.ValueSet("http://example.org/ValueSet/a"); !ok {
		t.Fatal("expected ValueSet to be indexed")
	}
	if _, ok := r.CodeSystem("http://example.org/CodeSystem/a"); !ok {
		t.Fatal("expected CodeSystem to be indexed")
	}
	if _, ok := r.SearchParameter("Observation", "code"); !ok {
		t.Fatal("expected SearchParameter to be indexed")
	}
	foundCapability := false
	for _, cs := range r.CapabilityStatements() {
		if cs.URL == "http://example.org/CapabilityStatement/a" {
			foundCapability = true
		}
	}
	if !foundCapability {
		t.Fatal("expected CapabilityStatement to be indexed")
	}
}

func TestRegistryBuilderBuildFromPackages(t *testing.T) {
	builder := NewRegistryBuilder()
	pkgs := []*Package{
		{
			Name:    "p1",
			Version: "1.0.0",
			Resources: []any{
				&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient-a", Type: "Patient"},
			},
		},
		{
			Name:    "p2",
			Version: "1.0.0",
			Resources: []any{
				&model.StructureDefinition{URL: "http://example.org/StructureDefinition/patient-b", Type: "Patient"},
				&model.ValueSet{URL: "http://example.org/ValueSet/b"},
			},
		},
	}

	r, err := builder.BuildFromPackages(pkgs)
	if err != nil {
		t.Fatalf("BuildFromPackages returned error: %v", err)
	}

	if _, ok := r.StructureDefinition("http://example.org/StructureDefinition/patient-a"); !ok {
		t.Fatal("expected first package StructureDefinition to be indexed")
	}
	if _, ok := r.StructureDefinition("http://example.org/StructureDefinition/patient-b"); !ok {
		t.Fatal("expected second package StructureDefinition to be indexed")
	}
	if _, ok := r.ValueSet("http://example.org/ValueSet/b"); !ok {
		t.Fatal("expected second package ValueSet to be indexed")
	}
}

func TestRegistryBuilderBuildFromPackagesScoped(t *testing.T) {
	builder := NewRegistryBuilder()
	root := &Package{
		Name:    "root",
		Version: "1.0.0",
		Resources: []any{
			&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-patient", Type: "Patient"},
		},
	}
	deps := []*Package{
		{
			Name:    "core",
			Version: "1.0.0",
			Resources: []any{
				&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Type: "Patient"},
				&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Observation", Type: "Observation"},
			},
		},
	}
	pkgs := append([]*Package{root}, deps...)

	r, err := builder.BuildFromPackagesScoped(pkgs, root)
	if err != nil {
		t.Fatalf("BuildFromPackagesScoped returned error: %v", err)
	}

	// Only the root package's StructureDefinitions are test subjects.
	scoped := r.ScopedStructureDefinitions()
	if len(scoped) != 1 {
		t.Fatalf("scoped subjects = %d, want 1", len(scoped))
	}
	if scoped[0].URL != "http://example.org/StructureDefinition/root-patient" {
		t.Fatalf("unexpected scoped subject %q", scoped[0].URL)
	}

	// Dependency definitions remain indexed and resolvable.
	if _, ok := r.StructureDefinition("http://hl7.org/fhir/StructureDefinition/Patient"); !ok {
		t.Fatal("dependency StructureDefinition should remain indexed for resolution")
	}
	if _, ok := r.StructureDefinition("http://hl7.org/fhir/StructureDefinition/Observation"); !ok {
		t.Fatal("dependency StructureDefinition should remain indexed for resolution")
	}
}

func TestRegistryBuilderBuildFromPackagesScopedNilRootUnscoped(t *testing.T) {
	builder := NewRegistryBuilder()
	pkgs := []*Package{
		{
			Name:    "root",
			Version: "1.0.0",
			Resources: []any{
				&model.StructureDefinition{URL: "http://example.org/StructureDefinition/root-patient", Type: "Patient"},
			},
		},
		{
			Name:    "core",
			Version: "1.0.0",
			Resources: []any{
				&model.StructureDefinition{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Type: "Patient"},
			},
		},
	}

	r, err := builder.BuildFromPackagesScoped(pkgs, nil)
	if err != nil {
		t.Fatalf("BuildFromPackagesScoped returned error: %v", err)
	}
	if got := len(r.ScopedStructureDefinitions()); got != 2 {
		t.Fatalf("nil root scoped subjects = %d, want 2 (unscoped)", got)
	}
}

func TestReadPackageRejectsOversizedEntry(t *testing.T) {
	// A small gzip that decompresses to more than the per-entry cap, simulating
	// a decompression/memory bomb.
	big := bytes.Repeat([]byte("a"), 64<<20+1)
	archivePath := buildTestPackageArchiveWithRawFiles(t, map[string][]byte{
		"package/package.json": mustMarshalJSON(t, map[string]any{
			"name":    "big.pkg",
			"version": "1.0.0",
		}),
		"package/ValueSet-big.json": big,
	})

	_, err := ReadPackage(archivePath)
	if err == nil {
		t.Fatal("expected oversized entry error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestReadPackageDeDuplicatesDependencies(t *testing.T) {
	archivePath := buildTestPackageArchive(t, map[string]any{
		"package/package.json": map[string]any{
			"name":    "dedupe.pkg",
			"version": "1.0.0",
			"dependencies": map[string]string{
				"b.pkg": "1.0.0",
				"c.pkg": "2.0.0",
			},
			"dependsOn": []map[string]any{
				{"name": "b.pkg", "version": "1.0.0"},
				{"name": "c.pkg", "version": "2.0.0"},
				{"name": "d.pkg", "version": "3.0.0"},
			},
		},
	})

	pkg, err := ReadPackage(archivePath)
	if err != nil {
		t.Fatalf("ReadPackage returned error: %v", err)
	}
	if len(pkg.Dependencies) != 3 {
		t.Fatalf("got %d dependencies, want 3 (de-duplicated)", len(pkg.Dependencies))
	}

	seen := make(map[string]int, len(pkg.Dependencies))
	for _, dep := range pkg.Dependencies {
		seen[dep.Name]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("dependency %s appears %d times, want 1", name, count)
		}
	}
}

func buildTestPackageArchive(t *testing.T, files map[string]any) string {
	t.Helper()
	rawFiles := make(map[string][]byte, len(files))
	for name, content := range files {
		rawFiles[name] = mustMarshalJSON(t, content)
	}
	return buildTestPackageArchiveWithRawFiles(t, rawFiles)
}

func buildTestPackageArchiveWithRawFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "package.tgz")

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

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal json: %v", err)
	}
	return b
}

func withUTF8BOM(b []byte) []byte {
	return append([]byte{0xEF, 0xBB, 0xBF}, bytes.TrimSpace(b)...)
}
