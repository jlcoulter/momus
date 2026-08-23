package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxOutputBackups is the number of timestamped versions of a replaced output
// directory that are retained before older ones are pruned.
var maxOutputBackups = 5

// writeVersionedDir writes a navigable output directory, first rotating any
// existing directory at path to path.<timestamp> so that prior results are
// preserved rather than silently overwritten. It prunes backups beyond
// maxOutputBackups. The dir writer fn is expected to (re)create path.
func writeVersionedDir(path string, fn func() error) error {
	if err := rotateDir(path); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "versioned: %s -> %s\n", path, latestBackup(path))
	return nil
}

// rotateDir renames an existing path to a timestamped backup and prunes old
// backups. It is a no-op when path does not already exist.
func rotateDir(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	backup := backupPath(path, time.Now())
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("rotate existing output %s to %s: %w", path, backup, err)
	}
	if err := pruneBackups(path); err != nil {
		return err
	}
	return nil
}

// backupPath produces a timestamped sibling path "<path>.<stamp>". If a backup
// with that timestamp already exists (e.g. two writes in the same second), a
// numeric suffix disambiguates it.
func backupPath(path string, t time.Time) string {
	base := path + "." + t.Format("2006-01-02T15-04-05")
	candidate := base
	for i := 1; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = base + "." + fmt.Sprintf("%d", i)
	}
}

// pruneBackups removes timestamped backups of path beyond maxOutputBackups,
// keeping the most recent.
func pruneBackups(path string) error {
	backups := listBackups(path)
	if len(backups) <= maxOutputBackups {
		return nil
	}
	for _, b := range backups[:len(backups)-maxOutputBackups] {
		if err := os.RemoveAll(b); err != nil {
			return fmt.Errorf("prune backup %s: %w", b, err)
		}
	}
	return nil
}

// listBackups returns timestamped siblings of path (i.e. "path.<stamp>") sorted
// by timestamp ascending.
func listBackups(path string) []string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := base + "."
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && name != base {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out
}

// latestBackup returns the most recent backup of path, or "" if none exist.
func latestBackup(path string) string {
	backups := listBackups(path)
	if len(backups) == 0 {
		return ""
	}
	return backups[len(backups)-1]
}
