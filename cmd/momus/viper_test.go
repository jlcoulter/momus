package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestViperPrecedence verifies that initializeViper applies config file values
// for keys not set on the CLI, and that explicitly-set CLI flags win over the
// config file.
func TestViperPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "momus.toml")
	if err := os.WriteFile(cfgPath, []byte("base_url = \"https://config.example.com\"\ninteraction_strength = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &config{}
	root := &cobra.Command{Use: "momus"}
	root.PersistentFlags().StringVar(&c.ConfigFile, "config", "", "")
	root.PersistentFlags().BoolVar(&c.Debug, "debug", false, "")
	sub := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	}}
	sub.Flags().StringVar(&c.BaseURL, "base-url", "", "")
	sub.Flags().StringVar(&c.WriteBaseURL, "write-base-url", "", "")
	sub.Flags().IntVar(&c.InteractionStrength, "strength", 1, "")
	root.AddCommand(sub)

	// PersistentPreRunE so the config is loaded before sub runs.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return initializeViper(c, cmd)
	}

	root.SetArgs([]string{"--config", cfgPath, "test", "--base-url", "https://cli.example.com"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	if c.BaseURL != "https://cli.example.com" {
		t.Errorf("base-url from CLI = %q, want cli.example.com", c.BaseURL)
	}
	if c.InteractionStrength != 2 {
		t.Errorf("interaction_strength = %d, want 2 from config", c.InteractionStrength)
	}
	if c.Debug {
		t.Error("debug should default to false")
	}
}

// TestViperEnvVars verifies MOMUS_* environment variables are honored when no
// CLI flag and no config file value is present. HOME is redirected so the
// auto-initialisation writes to a temp dir rather than the real user config.
func TestViperEnvVars(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	viper.Reset()
	t.Setenv("MOMUS_BASE_URL", "https://env.example.com")

	c := &config{}
	cmd := &cobra.Command{Use: "momus"}
	cmd.Flags().StringVar(&c.BaseURL, "base-url", "", "")
	if err := initializeViper(c, cmd); err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://env.example.com" {
		t.Errorf("base-url from env = %q, want env.example.com", c.BaseURL)
	}
}

// TestAutoInitHomeConfig verifies that a placeholder $HOME/.momus/config.toml
// is created on first run when no other config file is present, and that its
// values are honoured.
func TestAutoInitHomeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	viper.Reset()

	cfgPath := filepath.Join(home, userConfigPath)
	if fileExists(cfgPath) {
		t.Fatalf("home config should not exist yet: %s", cfgPath)
	}

	c := &config{}
	cmd := &cobra.Command{Use: "momus"}
	cmd.Flags().StringVar(&c.BaseURL, "base-url", "", "")
	if err := initializeViper(c, cmd); err != nil {
		t.Fatal(err)
	}

	if !fileExists(cfgPath) {
		t.Fatalf("expected auto-init to create %s", cfgPath)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("auto-init config file is empty")
	}
}
