package main

import (
	"fmt"
	"path/filepath"

	"github.com/jlcoulter/momus/internal/fhir/registry"
)

// buildRegistryForValidate builds a registry from the configured package
// archive (--package). When no package is given it returns an empty registry
// (validation then only succeeds against profiles resolvable from built-in
// sources, i.e. none). resourcePath is the resource file path: a relative
// --package is resolved against its directory, so running validate from any
// working directory finds a package sitting next to the resource.
func buildRegistryForValidate(cfg *config, resourcePath string) (*registry.Registry, error) {
	if cfg.packagePath == "" {
		return registry.New(), nil
	}
	pkgPath := cfg.packagePath
	if !filepath.IsAbs(pkgPath) {
		pkgPath = filepath.Join(filepath.Dir(resourcePath), pkgPath)
	}
	graph, reg, err := resolvePackageGraph(cfg, pkgPath)
	if err != nil {
		return nil, fmt.Errorf("resolve package %s: %w", cfg.packagePath, err)
	}
	_ = graph
	return reg, nil
}

// profileURLsFor returns the profile URLs to validate against: the --profile
// flags if given, otherwise the resource's meta.profile claims.
func profileURLsFor(cfg *config, resource map[string]any) []string {
	if len(cfg.profileURLs) > 0 {
		return cfg.profileURLs
	}
	meta, ok := resource["meta"].(map[string]any)
	if !ok {
		return nil
	}
	profiles, ok := meta["profile"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range profiles {
		if s, ok := p.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
