package fhirpackage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
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

	r, err := builder.Build(pkg)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
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
	if _, ok := r.CapabilityStatement("http://example.org/CapabilityStatement/a"); !ok {
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
