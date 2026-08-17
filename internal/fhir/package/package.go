// Package fhirpackage defines the abstraction for FHIR packages: named,
// versioned collections of FHIR resources plus their dependencies.
//
// The directory is named "package" to match the architecture, but the Go
// package name is "fhirpackage" because "package" is a reserved word.
package fhirpackage

import "context"

// Package is a loaded FHIR package.
type Package struct {
	Name         string
	Version      string
	Resources    []any
	Dependencies []Dependency
}

// Dependency identifies another package this package depends on.
type Dependency struct {
	Name    string
	Version string
}

// Source describes where a FHIR package should be loaded from.
type Source struct {
	// Name is the canonical package name, e.g. "hl7.fhir.r4.core".
	Name string
	// Version is the package version. Empty means "latest".
	Version string
	// LocalPath, when non-empty, points to a local package archive or
	// directory. Loading from it is not yet implemented.
	LocalPath string
}

// PackageLoader loads a FHIR package from a Source.
//
// Implementations will eventually download from a package registry or read
// from local archives.
type PackageLoader interface {
	Load(ctx context.Context, source Source) (*Package, error)
}
