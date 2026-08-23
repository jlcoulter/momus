// Package home resolves the single on-disk home directory that Momus uses for
// all persistent data: outputs, debug artifacts, downloaded package archives,
// and the per-user config file. Everything defaults to $HOME/.momus (or
// $MOMUS_HOME) so that Momus never writes into the current working directory
// unless the caller explicitly overrides a path with a flag.
package home

import (
	"os"
	"path/filepath"
)

// Sub directories (and files) under the Momus home directory.
const (
	ConfigFileName = "config.toml"
	OutputSubdir   = "output"
	PackagesSubdir = "packages"
)

// MomusHomeDir returns the base directory for all Momus data. It prefers the
// MOMUS_HOME environment variable, falling back to $HOME/.momus. The directory
// is created (with mode 0o700) if it does not yet exist.
func MomusHomeDir() string {
	if dir := os.Getenv("MOMUS_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(userHomeDir(), ".momus")
}

// ConfigPath returns the per-user config file path under the home directory.
func ConfigPath() string {
	return filepath.Join(MomusHomeDir(), ConfigFileName)
}

// OutputDir returns the default navigable output directory under the home
// directory.
func OutputDir() string {
	return filepath.Join(MomusHomeDir(), OutputSubdir)
}

// PackageCacheDir returns the default package download cache directory under
// the home directory.
func PackageCacheDir() string {
	return filepath.Join(MomusHomeDir(), PackagesSubdir)
}

func userHomeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
