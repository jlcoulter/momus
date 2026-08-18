package fhirpackage

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ResolvedGraph is the resolved dependency closure for a root package.
// Packages are ordered so dependencies always appear before dependents.
type ResolvedGraph struct {
	Packages []*Package
	// Root is the package the graph was resolved from. It is the subject of
	// test generation; the other packages in Packages are its dependencies,
	// used only to resolve referenced definitions.
	Root *Package
}

// ConflictPolicy controls how package version conflicts are handled.
type ConflictPolicy string

const (
	ConflictPolicyStrict   ConflictPolicy = "strict"
	ConflictPolicyRootWins ConflictPolicy = "root-wins"
)

// ResolveOptions configures dependency graph resolution.
type ResolveOptions struct {
	DepsDir        string
	DownloadDir    string
	ConflictPolicy ConflictPolicy
}

type registryPackageMetadata struct {
	DistTags map[string]string                     `json:"dist-tags"`
	Versions map[string]registryPackageVersionMeta `json:"versions"`
}

type registryPackageVersionMeta struct {
	Version string `json:"version"`
	Dist    struct {
		Tarball string `json:"tarball"`
	} `json:"dist"`
}

var packageRegistryBaseURLs = []string{
	"https://packages2.fhir.org/packages",
	"https://packages.simplifier.net",
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

type registryHTTPAuth struct {
	BasicUsername string
	BasicPassword string
	BearerToken   string
}

var registryAuth registryHTTPAuth

// ResolveLocalPackageGraphWithOptions resolves the root package and all
// transitive dependencies using the provided options.
func ResolveLocalPackageGraphWithOptions(rootArchivePath string, options ResolveOptions) (*ResolvedGraph, error) {
	if rootArchivePath == "" {
		return nil, errors.New("root archive path is required")
	}

	depsDir := options.DepsDir
	if depsDir == "" {
		depsDir = filepath.Dir(rootArchivePath)
	}

	downloadDir := options.DownloadDir
	if downloadDir == "" {
		downloadDir = filepath.Join(depsDir, ".momus", "packages")
	}

	policy := options.ConflictPolicy
	if policy == "" {
		policy = ConflictPolicyRootWins
	}

	index, err := IndexLocalPackageArchives(depsDir)
	if err != nil {
		return nil, err
	}
	if downloadDir != depsDir {
		downloadedIndex, err := IndexLocalPackageArchives(downloadDir)
		if err != nil {
			return nil, err
		}
		for key, archivePath := range downloadedIndex {
			if _, exists := index[key]; !exists {
				index[key] = archivePath
			}
		}
	}

	loaded := make(map[string]*Package)
	visiting := make(map[string]bool)
	selectedVersion := make(map[string]string)
	order := make([]string, 0)
	packageCache := make(map[string]*Package)

	loadPackage := func(archivePath string) (*Package, error) {
		if pkg, ok := packageCache[archivePath]; ok {
			return pkg, nil
		}
		pkg, err := ReadPackage(archivePath)
		if err != nil {
			return nil, fmt.Errorf("load package from %s: %w", archivePath, err)
		}
		packageCache[archivePath] = pkg
		return pkg, nil
	}

	rootPackage, err := loadPackage(rootArchivePath)
	if err != nil {
		return nil, err
	}
	if rootPackage.Name != "" && rootPackage.Version != "" {
		selectedVersion[rootPackage.Name] = rootPackage.Version
	}

	rootPreferred := make(map[string]string, len(rootPackage.Dependencies))
	for _, dep := range rootPackage.Dependencies {
		if dep.Name == "" || dep.Version == "" {
			continue
		}
		rootPreferred[dep.Name] = dep.Version
	}

	var dfs func(archivePath, expectedName, expectedVersion string) (string, error)
	dfs = func(archivePath, expectedName, expectedVersion string) (string, error) {
		pkg, err := loadPackage(archivePath)
		if err != nil {
			return "", err
		}
		if pkg.Name == "" || pkg.Version == "" {
			return "", fmt.Errorf("package in %s missing name or version", archivePath)
		}

		resolvedExpectedVersion, overriddenVersion, err := resolveRequestedVersion(pkg.Name, expectedVersion, selectedVersion, rootPreferred, policy)
		if err != nil {
			return "", err
		}
		if overriddenVersion && expectedVersion != "" {
			debug("overriding dependency version due to conflict policy", "packageName", pkg.Name, "requestedVersion", expectedVersion, "selectedVersion", resolvedExpectedVersion, "policy", string(policy))
		}

		if expectedName != "" && pkg.Name != expectedName {
			return "", fmt.Errorf("dependency name mismatch for %s: expected %s got %s", archivePath, expectedName, pkg.Name)
		}
		if resolvedExpectedVersion != "" && pkg.Version != resolvedExpectedVersion {
			return "", fmt.Errorf("dependency version mismatch for %s: expected %s got %s", archivePath, resolvedExpectedVersion, pkg.Version)
		}

		key := packageKey(pkg.Name, pkg.Version)
		if _, ok := loaded[key]; ok {
			return key, nil
		}
		if visiting[key] {
			warn("dependency cycle encountered; reusing in-progress package", "key", key, "archivePath", archivePath)
			return key, nil
		}

		if existingVersion, ok := selectedVersion[pkg.Name]; ok && existingVersion != pkg.Version {
			return "", fmt.Errorf("version conflict for package %s: %s vs %s", pkg.Name, existingVersion, pkg.Version)
		}
		selectedVersion[pkg.Name] = pkg.Version

		visiting[key] = true
		for _, dep := range sortedDependencies(pkg.Dependencies) {
			resolvedVersion, overridden, err := resolveRequestedVersion(dep.Name, dep.Version, selectedVersion, rootPreferred, policy)
			if err != nil {
				return "", err
			}
			depToLoad := dep
			if resolvedVersion != "" {
				depToLoad.Version = resolvedVersion
			}
			if overridden && dep.Version != "" {
				debug("overriding transitive dependency version due to conflict policy", "packageName", dep.Name, "requestedVersion", dep.Version, "selectedVersion", resolvedVersion, "policy", string(policy), "dependentPackage", pkg.Name)
			}

			depArchivePath, err := ensureDependencyArchive(index, downloadDir, depToLoad)
			if err != nil {
				return "", err
			}
			if _, err := dfs(depArchivePath, dep.Name, depToLoad.Version); err != nil {
				return "", err
			}
		}
		visiting[key] = false

		loaded[key] = pkg
		order = append(order, key)
		debug("resolved package", "key", key, "archivePath", archivePath)
		return key, nil
	}

	if _, err := dfs(rootArchivePath, "", ""); err != nil {
		return nil, err
	}

	result := &ResolvedGraph{Packages: make([]*Package, 0, len(order)), Root: rootPackage}
	for _, key := range order {
		result.Packages = append(result.Packages, loaded[key])
	}

	debug("resolved dependency graph", "packageCount", len(result.Packages), "depsDir", depsDir, "downloadDir", downloadDir, "conflictPolicy", string(policy))
	return result, nil
}

func resolveRequestedVersion(packageName, requestedVersion string, selectedVersion map[string]string, rootPreferred map[string]string, policy ConflictPolicy) (string, bool, error) {
	if requestedVersion == "" {
		if selected, ok := selectedVersion[packageName]; ok {
			return selected, false, nil
		}
		if preferred, ok := rootPreferred[packageName]; ok {
			return preferred, true, nil
		}
		return "", false, nil
	}

	if isFloatingVersion(requestedVersion) {
		if selected, ok := selectedVersion[packageName]; ok && selected != "" && !isFloatingVersion(selected) {
			return selected, true, nil
		}
		if preferred, ok := rootPreferred[packageName]; ok && preferred != "" && !isFloatingVersion(preferred) {
			selectedVersion[packageName] = preferred
			return preferred, true, nil
		}
		return "", true, nil
	}

	if selected, ok := selectedVersion[packageName]; ok {
		if selected == requestedVersion {
			return selected, false, nil
		}
		switch policy {
		case ConflictPolicyRootWins:
			return selected, true, nil
		case ConflictPolicyStrict:
			return "", false, fmt.Errorf("version conflict for package %s: %s vs %s", packageName, selected, requestedVersion)
		default:
			return "", false, fmt.Errorf("unsupported conflict policy %q", policy)
		}
	}

	if preferred, ok := rootPreferred[packageName]; ok && preferred != requestedVersion {
		switch policy {
		case ConflictPolicyRootWins:
			selectedVersion[packageName] = preferred
			return preferred, true, nil
		case ConflictPolicyStrict:
			return "", false, fmt.Errorf("version conflict for package %s: %s vs %s", packageName, preferred, requestedVersion)
		default:
			return "", false, fmt.Errorf("unsupported conflict policy %q", policy)
		}
	}

	selectedVersion[packageName] = requestedVersion
	return requestedVersion, false, nil
}

// IndexLocalPackageArchives scans depsDir recursively and indexes package
// archives by name@version.
func IndexLocalPackageArchives(depsDir string) (map[string]string, error) {
	if depsDir == "" {
		return nil, errors.New("dependency directory is required")
	}

	index := make(map[string]string)
	if _, statErr := os.Stat(depsDir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return index, nil
		}
		return nil, fmt.Errorf("stat dependency directory %s: %w", depsDir, statErr)
	}

	err := filepath.WalkDir(depsDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isPackageArchivePath(p) {
			return nil
		}

		manifest, err := readPackageManifestFromArchive(p)
		if err != nil {
			debug("skipping archive without readable package manifest", "archivePath", p, "error", err)
			return nil
		}
		if manifest.Name == "" || manifest.Version == "" {
			debug("skipping archive with incomplete package manifest", "archivePath", p)
			return nil
		}

		key := packageKey(manifest.Name, manifest.Version)
		if existing, exists := index[key]; exists {
			debug("duplicate package archive encountered", "key", key, "kept", existing, "ignored", p)
			return nil
		}
		index[key] = p
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("index dependency archives under %s: %w", depsDir, err)
	}

	return index, nil
}

func readPackageManifestFromArchive(archivePath string) (*packageManifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.ToLower(path.Base(hdr.Name)) != "package.json" {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		var manifest packageManifest
		if err := json.Unmarshal(normalizeJSON(data), &manifest); err != nil {
			return nil, err
		}
		return &manifest, nil
	}

	return nil, errors.New("package.json not found")
}

func findDependencyArchive(index map[string]string, dep Dependency) (string, error) {
	if dep.Name == "" {
		return "", errors.New("dependency has empty name")
	}

	if dep.Version != "" && !isFloatingVersion(dep.Version) {
		key := packageKey(dep.Name, dep.Version)
		if p, ok := index[key]; ok {
			return p, nil
		}
		return "", fmt.Errorf("dependency archive not found for %s", key)
	}

	prefix := dep.Name + "@"
	matches := make([]string, 0, 1)
	for key, p := range index {
		if strings.HasPrefix(key, prefix) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("dependency archive not found for %s (version unspecified)", dep.Name)
	}
	return "", fmt.Errorf("dependency %s is ambiguous; multiple versions found", dep.Name)
}

func ensureDependencyArchive(index map[string]string, downloadDir string, dep Dependency) (string, error) {
	p, err := findDependencyArchive(index, dep)
	if err == nil {
		return p, nil
	}

	if dep.Version == "" {
		return "", err
	}

	debug("dependency archive not found locally, attempting remote fetch", "name", dep.Name, "version", dep.Version, "downloadDir", downloadDir)
	resolvedDep, downloadedPath, fetchErr := fetchDependencyArchive(dep, downloadDir)
	if fetchErr != nil {
		return "", err
	}

	index[packageKey(resolvedDep.Name, resolvedDep.Version)] = downloadedPath
	if resolvedDep.Version != dep.Version {
		index[packageKey(dep.Name, dep.Version)] = downloadedPath
	}
	return downloadedPath, nil
}

func fetchDependencyArchive(dep Dependency, downloadDir string) (Dependency, string, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return Dependency{}, "", fmt.Errorf("create dependency directory %s: %w", downloadDir, err)
	}

	resolvedDep, tarballURL, err := resolveRemoteDependency(dep)
	if err != nil {
		return Dependency{}, "", err
	}

	fileName := sanitizeFileComponent(resolvedDep.Name) + "-" + sanitizeFileComponent(resolvedDep.Version) + ".tgz"
	destination := filepath.Join(downloadDir, fileName)
	if _, statErr := os.Stat(destination); statErr == nil {
		return resolvedDep, destination, nil
	}

	debug("fetching dependency archive", "url", tarballURL, "destination", destination, "requestedVersion", dep.Version, "resolvedVersion", resolvedDep.Version)
	if err := downloadFile(tarballURL, destination); err != nil {
		return Dependency{}, "", fmt.Errorf("fetch dependency %s failed: %w", packageKey(dep.Name, dep.Version), err)
	}

	manifest, err := readPackageManifestFromArchive(destination)
	if err != nil {
		_ = os.Remove(destination)
		return Dependency{}, "", fmt.Errorf("downloaded archive was invalid: %w", err)
	}
	if manifest.Name != resolvedDep.Name || manifest.Version != resolvedDep.Version {
		_ = os.Remove(destination)
		return Dependency{}, "", fmt.Errorf("downloaded archive mismatch got %s@%s", manifest.Name, manifest.Version)
	}

	debug("downloaded dependency archive", "name", resolvedDep.Name, "version", resolvedDep.Version, "destination", destination)
	return resolvedDep, destination, nil
}

func resolveRemoteDependency(dep Dependency) (Dependency, string, error) {
	var errs []string
	for _, baseURL := range packageRegistryBaseURLs {
		metadataURL := strings.TrimRight(baseURL, "/") + "/" + dep.Name
		meta, err := fetchRegistryPackageMetadata(metadataURL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", metadataURL, err))
			continue
		}

		version, tarballURL, err := resolveVersionFromMetadata(dep, meta)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", metadataURL, err))
			continue
		}

		return Dependency{Name: dep.Name, Version: version}, tarballURL, nil
	}

	return Dependency{}, "", fmt.Errorf("resolve remote dependency %s failed: %s", packageKey(dep.Name, dep.Version), strings.Join(errs, "; "))
}

func fetchRegistryPackageMetadata(metadataURL string) (*registryPackageMetadata, error) {
	req, err := http.NewRequest(http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, err
	}
	applyRegistryAuth(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var meta registryPackageMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func resolveVersionFromMetadata(dep Dependency, meta *registryPackageMetadata) (string, string, error) {
	if meta == nil {
		return "", "", errors.New("package metadata is nil")
	}

	requestedVersion := dep.Version
	resolvedVersion := requestedVersion
	if isFloatingVersion(requestedVersion) {
		tagName := floatingVersionTag(requestedVersion)
		if tagVersion, ok := meta.DistTags[tagName]; ok && tagVersion != "" {
			resolvedVersion = tagVersion
		} else if latest, ok := meta.DistTags["latest"]; ok && latest != "" {
			resolvedVersion = latest
		} else {
			return "", "", fmt.Errorf("floating version %q could not be resolved", requestedVersion)
		}
	}

	versionMeta, ok := meta.Versions[resolvedVersion]
	if !ok {
		return "", "", fmt.Errorf("version %q not found in registry metadata", resolvedVersion)
	}
	if versionMeta.Dist.Tarball == "" {
		return "", "", fmt.Errorf("version %q missing dist.tarball", resolvedVersion)
	}

	return resolvedVersion, versionMeta.Dist.Tarball, nil
}

func downloadFile(downloadURL, destination string) error {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	applyRegistryAuth(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	tmpDestination := destination + ".part"
	f, err := os.Create(tmpDestination)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = os.Remove(tmpDestination)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpDestination)
		return err
	}

	if err := os.Rename(tmpDestination, destination); err != nil {
		_ = os.Remove(tmpDestination)
		return err
	}
	return nil
}

func applyRegistryAuth(req *http.Request) {
	if req == nil {
		return
	}
	if registryAuth.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+registryAuth.BearerToken)
		return
	}
	if registryAuth.BasicUsername != "" || registryAuth.BasicPassword != "" {
		req.SetBasicAuth(registryAuth.BasicUsername, registryAuth.BasicPassword)
	}
}

func sortedDependencies(deps []Dependency) []Dependency {
	if len(deps) == 0 {
		return nil
	}
	uniq := make(map[string]Dependency, len(deps))
	for _, dep := range deps {
		if dep.Name == "" {
			continue
		}
		key := packageKey(dep.Name, dep.Version)
		uniq[key] = dep
	}

	out := make([]Dependency, 0, len(uniq))
	for _, dep := range uniq {
		out = append(out, dep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Version < out[j].Version
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func packageKey(name, version string) string {
	return name + "@" + version
}

func isPackageArchivePath(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".tgz") || strings.HasSuffix(l, ".tar.gz")
}

func isFloatingVersion(version string) bool {
	if version == "" {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(version))
	return v == "current" || v == "latest" || strings.Contains(v, "x") || strings.Contains(v, "*")
}

func floatingVersionTag(version string) string {
	v := strings.ToLower(strings.TrimSpace(version))
	switch v {
	case "current":
		return "latest"
	default:
		return v
	}
}

func sanitizeFileComponent(s string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return replacer.Replace(s)
}
