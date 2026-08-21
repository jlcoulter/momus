// Package fhirpackage defines the abstraction for FHIR packages: named,
// versioned collections of FHIR resources plus their dependencies.
//
// The directory is named "package" to match the architecture, but the Go
// package name is "fhirpackage" because "package" is a reserved word.
package fhirpackage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/model"
)

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

var logLevel = &slog.LevelVar{}

var logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

// SetDebug toggles verbose debug logging for this package.
func SetDebug(enabled bool) {
	if enabled {
		logLevel.Set(slog.LevelDebug)
		return
	}
	logLevel.Set(slog.LevelInfo)
}

func debug(msg string, args ...any) {
	logger.Debug(msg, args...)
}

func warn(msg string, args ...any) {
	logger.Warn(msg, args...)
}

type packageManifest struct {
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	Dependencies map[string]string    `json:"dependencies"`
	DepsArray    []manifestDependency `json:"dependsOn"`
}

type manifestDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type resourceEnvelope struct {
	ResourceType string `json:"resourceType"`
}

type structureDefinitionJSON struct {
	URL            string `json:"url"`
	Version        string `json:"version"`
	Name           string `json:"name"`
	Title          string `json:"title"`
	Type           string `json:"type"`
	BaseDefinition string `json:"baseDefinition"`
	Kind           string `json:"kind"`
	Derivation     string `json:"derivation"`
	Snapshot       struct {
		Element []map[string]any `json:"element"`
	} `json:"snapshot"`
	Differential struct {
		Element []map[string]any `json:"element"`
	} `json:"differential"`
}

type valueSetJSON struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Compose struct {
		Include []struct {
			System  string `json:"system"`
			Concept []struct {
				Code    string `json:"code"`
				Display string `json:"display"`
			} `json:"concept"`
		} `json:"include"`
	} `json:"compose"`
	Expansion struct {
		Contains []struct {
			System   string `json:"system"`
			Code     string `json:"code"`
			Display  string `json:"display"`
			Contains []struct {
				System   string `json:"system"`
				Code     string `json:"code"`
				Display  string `json:"display"`
				Contains []struct {
					System  string `json:"system"`
					Code    string `json:"code"`
					Display string `json:"display"`
				} `json:"contains"`
			} `json:"contains"`
		} `json:"contains"`
	} `json:"expansion"`
}

type codeSystemJSON struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Concept []struct {
		Code    string `json:"code"`
		Display string `json:"display"`
		Concept []struct {
			Code    string `json:"code"`
			Display string `json:"display"`
			Concept []struct {
				Code    string `json:"code"`
				Display string `json:"display"`
			} `json:"concept"`
		} `json:"concept"`
	} `json:"concept"`
}

type capabilityStatementJSON struct {
	URL         string `json:"url"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	FhirVersion string `json:"fhirVersion"`
	Rest        []struct {
		Mode     string `json:"mode"`
		Resource []struct {
			Type             string   `json:"type"`
			Profile          string   `json:"profile"`
			SupportedProfile []string `json:"supportedProfile"`
			Interaction      []struct {
				Code string `json:"code"`
			} `json:"interaction"`
			Operation []struct {
				Name       string `json:"name"`
				Definition string `json:"definition"`
			} `json:"operation"`
		} `json:"resource"`
	} `json:"rest"`
}

type searchParameterJSON struct {
	URL        string   `json:"url"`
	Name       string   `json:"name"`
	Code       string   `json:"code"`
	Base       []string `json:"base"`
	Type       string   `json:"type"`
	Expression string   `json:"expression"`
}

// ReadPackage reads a FHIR package from a .tgz archive.
func ReadPackage(packagePath string) (*Package, error) {
	debug("reading package archive", "packagePath", packagePath)

	file, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	pkg := &Package{}
	entriesRead := 0
	jsonEntries := 0
	decodedResources := 0
	skippedResources := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return nil, fmt.Errorf("read archive entry: %w", err)
		}
		entriesRead++
		debug("archive entry", "name", header.Name, "type", header.Typeflag, "size", header.Size)

		if header.Typeflag != tar.TypeReg {
			debug("skipping non-regular entry", "name", header.Name)
			continue
		}

		if !strings.HasSuffix(strings.ToLower(header.Name), ".json") {
			debug("skipping non-json entry", "name", header.Name)
			continue
		}
		jsonEntries++

		// maxArchiveEntrySize caps the decompressed size of a single archive entry.
		// Each entry is fully buffered in memory (see ReadPackage), so this bound
		// keeps the in-memory footprint of a package archive bounded and prevents a
		// large or hostile .tgz from acting as a decompression/memory bomb.
		const maxArchiveEntrySize = 64 << 20 // 64 MiB per entry

		contents, err := io.ReadAll(io.LimitReader(tr, maxArchiveEntrySize+1))
		if err != nil {
			return nil, fmt.Errorf("read archive entry content: %w", err)
		}
		if len(contents) > maxArchiveEntrySize {
			return nil, fmt.Errorf("archive entry %s exceeds maximum size of %d bytes", header.Name, maxArchiveEntrySize)
		}
		contents = normalizeJSON(contents)

		base := strings.ToLower(path.Base(header.Name))
		if base == "package.json" {
			debug("decoding manifest", "name", header.Name)
			if err := decodeManifest(contents, pkg); err != nil {
				return nil, fmt.Errorf("decode manifest %s: %w", header.Name, err)
			}
			debug("manifest decoded", "name", pkg.Name, "version", pkg.Version, "dependencies", len(pkg.Dependencies))
			continue
		}

		res, err := decodeResource(contents)
		if err != nil {
			// Non-FHIR JSON files may exist in package archives; skip those.
			skippedResources++
			debug("skipping non-fhir-or-invalid-json resource", "name", header.Name, "error", err)
			continue
		}
		// A decoded instance Resource with an empty ResourceType is a non-FHIR
		// JSON file that happened to parse (no "resourceType"); skip it. Other
		// resources are retained, including instance/example resources now that
		// the registry represents the package in full.
		if instance, ok := res.(*model.Resource); ok && instance.ResourceType == "" {
			skippedResources++
			debug("skipping json resource without a resourceType", "name", header.Name)
			continue
		}
		if res != nil {
			pkg.Resources = append(pkg.Resources, res)
			decodedResources++
			debug("decoded resource", "name", header.Name, "type", fmt.Sprintf("%T", res))
		} else {
			skippedResources++
			debug("skipping unsupported FHIR resource", "name", header.Name)
		}
	}

	if pkg.Name == "" {
		warn("package name is empty after archive read", "packagePath", packagePath)
	}

	debug("package read complete", "entries", entriesRead, "jsonEntries", jsonEntries, "decodedResources", decodedResources, "skippedResources", skippedResources, "dependencies", len(pkg.Dependencies))

	return pkg, nil
}

func decodeManifest(data []byte, pkg *Package) error {
	var m packageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	pkg.Name = m.Name
	pkg.Version = m.Version

	// A manifest may declare dependencies via both the "dependencies" map and
	// the "dependsOn" array; de-dupe on decode so the same dependency is not
	// recorded twice.
	seen := make(map[string]struct{}, len(m.Dependencies)+len(m.DepsArray))
	add := func(name, version string) {
		if name == "" {
			return
		}
		key := packageKey(name, version)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		pkg.Dependencies = append(pkg.Dependencies, Dependency{Name: name, Version: version})
	}

	for name, version := range m.Dependencies {
		add(name, version)
	}
	for _, dep := range m.DepsArray {
		add(dep.Name, dep.Version)
	}

	return nil
}

// decodeResource decodes a FHIR resource from JSON into a specific struct based on its resourceType.
func decodeResource(data []byte) (any, error) {
	var env resourceEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}

	switch env.ResourceType {
	case "StructureDefinition":
		var sd structureDefinitionJSON
		if err := json.Unmarshal(data, &sd); err != nil {
			return nil, err
		}
		elements := sd.Snapshot.Element
		if len(elements) == 0 {
			elements = sd.Differential.Element
		}
		return &model.StructureDefinition{
			URL:            sd.URL,
			Version:        sd.Version,
			Name:           sd.Name,
			Title:          sd.Title,
			Type:           sd.Type,
			BaseDefinition: sd.BaseDefinition,
			Kind:           sd.Kind,
			Derivation:     sd.Derivation,
			Elements:       decodeElementDefinitions(elements),
		}, nil
	case "ValueSet":
		var vs valueSetJSON
		if err := json.Unmarshal(data, &vs); err != nil {
			return nil, err
		}
		includes := make([]model.ValueSetInclude, 0, len(vs.Compose.Include))
		for _, include := range vs.Compose.Include {
			concepts := make([]model.ConceptReference, 0, len(include.Concept))
			for _, concept := range include.Concept {
				concepts = append(concepts, model.ConceptReference{Code: concept.Code, Display: concept.Display})
			}
			includes = append(includes, model.ValueSetInclude{System: include.System, Concepts: concepts})
		}
		return &model.ValueSet{
			URL:               vs.URL,
			Version:           vs.Version,
			Name:              vs.Name,
			Status:            vs.Status,
			ComposeIncludes:   includes,
			ExpansionContains: decodeExpansionContains(vs.Expansion.Contains),
		}, nil
	case "CodeSystem":
		var cs codeSystemJSON
		if err := json.Unmarshal(data, &cs); err != nil {
			return nil, err
		}
		return &model.CodeSystem{URL: cs.URL, Version: cs.Version, Name: cs.Name, Status: cs.Status, Concepts: decodeCodeSystemConcepts(cs.Concept)}, nil
	case "CapabilityStatement":
		var cs capabilityStatementJSON
		if err := json.Unmarshal(data, &cs); err != nil {
			return nil, err
		}
		rest := make([]model.CapabilityStatementRest, 0, len(cs.Rest))
		for _, restBlock := range cs.Rest {
			resources := make([]model.CapabilityStatementRestResource, 0, len(restBlock.Resource))
			for _, resource := range restBlock.Resource {
				interactions := make([]model.CapabilityStatementInteraction, 0, len(resource.Interaction))
				for _, interaction := range resource.Interaction {
					interactions = append(interactions, model.CapabilityStatementInteraction{Code: interaction.Code})
				}
				operations := make([]model.CapabilityStatementOperation, 0, len(resource.Operation))
				for _, operation := range resource.Operation {
					operations = append(operations, model.CapabilityStatementOperation{Name: operation.Name, Definition: operation.Definition})
				}
				resources = append(resources, model.CapabilityStatementRestResource{
					Type:             resource.Type,
					Profile:          resource.Profile,
					SupportedProfile: resource.SupportedProfile,
					Interaction:      interactions,
					Operation:        operations,
				})
			}
			rest = append(rest, model.CapabilityStatementRest{
				Mode:     restBlock.Mode,
				Resource: resources,
			})
		}
		return &model.CapabilityStatement{
			URL:         cs.URL,
			Version:     cs.Version,
			Name:        cs.Name,
			Status:      cs.Status,
			FhirVersion: cs.FhirVersion,
			Rest:        rest,
		}, nil
	case "SearchParameter":
		var sp searchParameterJSON
		if err := json.Unmarshal(data, &sp); err != nil {
			return nil, err
		}
		return &model.SearchParameter{
			URL:        sp.URL,
			Name:       sp.Name,
			Code:       sp.Code,
			Base:       sp.Base,
			Type:       sp.Type,
			Expression: sp.Expression,
		}, nil
	default:
		// Any other resource type is an instance resource (e.g. an example
		// Patient, Practitioner, or PractitionerRole shipped in the package).
		// Keep it as an opaque Resource so the registry can index and traverse
		// package example data as a source of conformant values. Non-FHIR JSON
		// files (no resourceType) fall through to this branch with an empty
		// ResourceType; the caller skips those via the returned error below.
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		return &model.Resource{
			ResourceType: env.ResourceType,
			ProfileURLs:  decodeMetaProfiles(raw),
			Raw:          raw,
		}, nil
	}
}

// decodeMetaProfiles extracts the canonical profile URLs declared in a
// resource's meta.profile.
func decodeMetaProfiles(raw map[string]any) []string {
	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		return nil
	}
	profiles, ok := meta["profile"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		if s, ok := p.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// decodeElementDefinitions decodes a slice of raw element definitions into a slice of model.ElementDefinition.
func decodeElementDefinitions(rawElements []map[string]any) []model.ElementDefinition {
	defs := make([]model.ElementDefinition, 0, len(rawElements))
	for _, elem := range rawElements {
		def := model.ElementDefinition{
			ID:          stringField(elem, "id"),
			Path:        stringField(elem, "path"),
			Name:        lastPathPart(stringField(elem, "path")),
			Min:         intField(elem, "min"),
			Max:         stringField(elem, "max"),
			BaseMax:     stringField(mapField(elem, "base"), "max"),
			MustSupport: boolField(elem, "mustSupport"),
			SliceName:   stringField(elem, "sliceName"),
		}

		if t, ok := elem["type"].([]any); ok {
			for _, rawType := range t {
				m, ok := rawType.(map[string]any)
				if !ok {
					continue
				}
				def.Types = append(def.Types, model.ElementType{
					Code:          stringField(m, "code"),
					Profile:       stringSliceField(m, "profile"),
					TargetProfile: stringSliceField(m, "targetProfile"),
				})
			}
		}

		if b, ok := elem["binding"].(map[string]any); ok {
			def.Binding = &model.Binding{
				Strength: stringField(b, "strength"),
				ValueSet: stringField(b, "valueSet"),
			}
		}

		if rawConstraints, ok := elem["constraint"].([]any); ok {
			def.Constraints = decodeConstraints(rawConstraints)
		}

		def.Profile = stringSliceField(elem, "profile")
		def.TargetProfile = stringSliceField(elem, "targetProfile")

		for k, v := range elem {
			if strings.HasPrefix(k, "fixed") {
				def.Fixed = v
			}
			if strings.HasPrefix(k, "pattern") {
				def.Pattern = v
			}
		}

		if rawExamples, ok := elem["example"].([]any); ok {
			def.Examples = decodeExamples(rawExamples)
		}

		defs = append(defs, def)
	}
	return defs
}

func decodeConstraints(rawConstraints []any) []model.ElementConstraint {
	constraints := make([]model.ElementConstraint, 0, len(rawConstraints))
	for _, raw := range rawConstraints {
		constraintMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		constraints = append(constraints, model.ElementConstraint{
			Key:        stringField(constraintMap, "key"),
			Severity:   stringField(constraintMap, "severity"),
			Human:      stringField(constraintMap, "human"),
			Expression: stringField(constraintMap, "expression"),
			Source:     stringField(constraintMap, "source"),
		})
	}
	return constraints
}

func decodeExamples(rawExamples []any) []any {
	examples := make([]any, 0, len(rawExamples))
	for _, raw := range rawExamples {
		exampleMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range exampleMap {
			if strings.HasPrefix(key, "value") {
				examples = append(examples, value)
				break
			}
		}
	}
	return examples
}

func decodeExpansionContains(raw []struct {
	System   string `json:"system"`
	Code     string `json:"code"`
	Display  string `json:"display"`
	Contains []struct {
		System   string `json:"system"`
		Code     string `json:"code"`
		Display  string `json:"display"`
		Contains []struct {
			System  string `json:"system"`
			Code    string `json:"code"`
			Display string `json:"display"`
		} `json:"contains"`
	} `json:"contains"`
}) []model.ValueSetExpansionContains {
	out := make([]model.ValueSetExpansionContains, 0, len(raw))
	for _, entry := range raw {
		childContains := make([]model.ValueSetExpansionContains, 0, len(entry.Contains))
		for _, child := range entry.Contains {
			grandChildren := make([]model.ValueSetExpansionContains, 0, len(child.Contains))
			for _, grandChild := range child.Contains {
				grandChildren = append(grandChildren, model.ValueSetExpansionContains{System: grandChild.System, Code: grandChild.Code, Display: grandChild.Display})
			}
			childContains = append(childContains, model.ValueSetExpansionContains{System: child.System, Code: child.Code, Display: child.Display, Contains: grandChildren})
		}
		out = append(out, model.ValueSetExpansionContains{System: entry.System, Code: entry.Code, Display: entry.Display, Contains: childContains})
	}
	return out
}

func decodeCodeSystemConcepts(raw []struct {
	Code    string `json:"code"`
	Display string `json:"display"`
	Concept []struct {
		Code    string `json:"code"`
		Display string `json:"display"`
		Concept []struct {
			Code    string `json:"code"`
			Display string `json:"display"`
		} `json:"concept"`
	} `json:"concept"`
}) []model.CodeSystemConcept {
	out := make([]model.CodeSystemConcept, 0, len(raw))
	for _, concept := range raw {
		children := make([]model.CodeSystemConcept, 0, len(concept.Concept))
		for _, child := range concept.Concept {
			grandChildren := make([]model.CodeSystemConcept, 0, len(child.Concept))
			for _, grandChild := range child.Concept {
				grandChildren = append(grandChildren, model.CodeSystemConcept{Code: grandChild.Code, Display: grandChild.Display})
			}
			children = append(children, model.CodeSystemConcept{Code: child.Code, Display: child.Display, Concepts: grandChildren})
		}
		out = append(out, model.CodeSystemConcept{Code: concept.Code, Display: concept.Display, Concepts: children})
	}
	return out
}

// stringField retrieves a string field from a map, returning an empty string if the key is not present or not a string.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// intField retrieves an integer field from a map, returning 0 if the key is not present or not a number.
func intField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// boolField retrieves a boolean field from a map, returning false if the key is not present or not a boolean.
func boolField(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func mapField(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	child, _ := v.(map[string]any)
	return child
}

// stringSliceField retrieves a slice of strings from a map, returning nil if the key is not present or not a slice of strings.
func stringSliceField(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}

	arr, ok := v.([]any)
	if !ok {
		if s, ok := v.(string); ok {
			return []string{s}
		}
		return nil
	}

	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// lastPathPart returns the last part of a path, stripping any file extension.
func lastPathPart(s string) string {
	idx := strings.LastIndex(s, "/")
	if idx >= 0 && idx+1 < len(s) {
		s = s[idx+1:]
	}
	idx = strings.LastIndex(s, ".")
	if idx > 0 {
		s = s[:idx]
	}
	return s
}

// normalizeJSON trims whitespace and removes a UTF-8 BOM from the beginning of JSON data.
func normalizeJSON(data []byte) []byte {
	// Some package files are UTF-8 JSON with BOM; strip it before decoding.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return bytes.TrimSpace(data)
}
