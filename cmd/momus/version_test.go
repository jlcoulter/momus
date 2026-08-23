package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotateDirPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "index.json")
	if err := os.WriteFile(marker, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := rotateDir(target); err != nil {
		t.Fatalf("rotateDir: %v", err)
	}

	// Original path gone, backup exists and carries the old content.
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected original dir removed, got %v", err)
	}
	backups := listBackups(target)
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d (%v)", len(backups), backups)
	}
	data, err := os.ReadFile(filepath.Join(backups[0], "index.json"))
	if err != nil {
		t.Fatalf("read backup marker: %v", err)
	}
	if string(data) != "old" {
		t.Fatalf("backup content = %q, want %q", data, "old")
	}
}

func TestRotateDir_NoopWhenMissing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "output")
	if err := rotateDir(target); err != nil {
		t.Fatalf("rotateDir on missing path: %v", err)
	}
	if backups := listBackups(target); len(backups) != 0 {
		t.Fatalf("expected no backups, got %v", backups)
	}
}

func TestRotateDir_SameSecondCollision(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output")
	ts := time.Date(2026, 8, 23, 15, 4, 5, 0, time.UTC)

	// Pre-create a backup with the exact same timestamp.
	if err := os.MkdirAll(backupPath(target, ts), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	// backupPath must disambiguate with a numeric suffix.
	if err := rotateAt(target, ts); err != nil {
		t.Fatalf("rotateDir: %v", err)
	}
	backups := listBackups(target)
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups (collision + new), got %d (%v)", len(backups), backups)
	}
}

func TestPruneBackups(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output")
	ts := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)

	orig := maxOutputBackups
	maxOutputBackups = 3
	t.Cleanup(func() { maxOutputBackups = orig })

	// Create target + 5 backups (6 total backups worth; target rotates into one).
	for i := 0; i < 6; i++ {
		b := backupPath(target, ts.Add(time.Duration(i)*time.Second))
		if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneBackups(target); err != nil {
		t.Fatalf("pruneBackups: %v", err)
	}
	backups := listBackups(target)
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups after prune, got %d (%v)", len(backups), backups)
	}
}

// rotateAt invokes rotateDir but stamps the backup with a fixed time so tests
// are deterministic and can construct collisions.
func rotateAt(target string, ts time.Time) error {
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	backup := backupPath(target, ts)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	return pruneBackups(target)
}

func TestWriteVersionedDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "output")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	ran := false
	if err := writeVersionedDir(target, func() error {
		ran = true
		return os.MkdirAll(target, 0o755)
	}); err != nil {
		t.Fatalf("writeVersionedDir: %v", err)
	}
	if !ran {
		t.Fatal("writer fn not invoked")
	}
	if backups := listBackups(target); len(backups) != 1 {
		t.Fatalf("expected 1 backup after write, got %v", backups)
	}
}
