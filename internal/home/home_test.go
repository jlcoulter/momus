package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMomusHomeDirPrefersMOMUSHOME(t *testing.T) {
	t.Setenv("MOMUS_HOME", "/custom/momus")
	t.Setenv("HOME", "/home/user")

	if got := MomusHomeDir(); got != "/custom/momus" {
		t.Fatalf("MomusHomeDir() = %q, want /custom/momus", got)
	}
}

func TestMomusHomeDirFallsBackToHomeDotMomus(t *testing.T) {
	t.Setenv("MOMUS_HOME", "")
	t.Setenv("HOME", "/home/user")

	if got := MomusHomeDir(); got != filepath.Join("/home/user", ".momus") {
		t.Fatalf("MomusHomeDir() = %q, want %q", got, filepath.Join("/home/user", ".momus"))
	}
}

func TestConfigPath(t *testing.T) {
	t.Setenv("MOMUS_HOME", "/custom/momus")
	t.Setenv("HOME", "/home/user")

	want := filepath.Join("/custom/momus", ConfigFileName)
	if got := ConfigPath(); got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestOutputDir(t *testing.T) {
	t.Setenv("MOMUS_HOME", "/custom/momus")
	t.Setenv("HOME", "/home/user")

	want := filepath.Join("/custom/momus", OutputSubdir)
	if got := OutputDir(); got != want {
		t.Fatalf("OutputDir() = %q, want %q", got, want)
	}
}

func TestPackageCacheDir(t *testing.T) {
	t.Setenv("MOMUS_HOME", "/custom/momus")
	t.Setenv("HOME", "/home/user")

	want := filepath.Join("/custom/momus", PackagesSubdir)
	if got := PackageCacheDir(); got != want {
		t.Fatalf("PackageCacheDir() = %q, want %q", got, want)
	}
}

func TestUserHomeDirPrefersHomeEnv(t *testing.T) {
	t.Setenv("HOME", "/custom/home")

	if got := userHomeDir(); got != "/custom/home" {
		t.Fatalf("userHomeDir() = %q, want /custom/home", got)
	}
}

func TestUserHomeDirFallsBackToOSLookup(t *testing.T) {
	t.Setenv("HOME", "")

	got := userHomeDir()
	expected, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir unavailable: %v", err)
	}
	if got != expected {
		t.Fatalf("userHomeDir() = %q, want %q", got, expected)
	}
}
