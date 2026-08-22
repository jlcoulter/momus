package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlcoulter/momus/internal/fhir/golden"
	"github.com/spf13/cobra"
)

// newConformanceSelfTestCmd returns the "conformance self-test" command, which
// runs the golden-matrix runner against every reference fixture and reports
// pass/skip. It is the CLI front-end for internal/fhir/golden: a deterministic,
// zero-network, CI-runnable oracle that proves Momus's own generated tests pass
// against the semantic mock. Exits non-zero on any failure.
func newConformanceSelfTestCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self-test",
		Short: "Run the golden-matrix self-conformance suite against reference fixtures",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Locate the repo testdata/golden dir relative to the working dir,
			// with a fallback search upward from CWD.
			dir := cfg.goldenDir
			if dir == "" {
				dir = findGoldenDir()
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return fmt.Errorf("read golden dir %s: %w", dir, err)
			}
			var fixtures []string
			for _, e := range entries {
				name := e.Name()
				if filepath.Ext(name) == ".json" && !strings.HasPrefix(name, ".") && !hasPlanSuffix(name) {
					fixtures = append(fixtures, filepath.Base(name[:len(name)-5]))
				}
			}
			if len(fixtures) == 0 {
				return fmt.Errorf("no reference fixtures found in %s", dir)
			}

			ctx := context.Background()
			failed := 0

			var out *os.File
			if cfg.conformanceOut != "" {
				out, err = os.Create(cfg.conformanceOut)
				if err != nil {
					return fmt.Errorf("create output file %s: %w", cfg.conformanceOut, err)
				}
				defer out.Close()
			}
			// logf writes to stdout and, when configured, appends the same line
			// to the detail output file so a run can be inspected after the fact.
			logf := func(format string, args ...any) {
				fmt.Printf(format, args...)
				if out != nil {
					fmt.Fprintf(out, format, args...)
				}
			}

			for _, name := range fixtures {
				fx, err := golden.LoadFixture(filepath.Join(dir, name+".json"))
				if err != nil {
					logf("[FAIL] %s: load fixture: %v\n", name, err)
					failed++
					continue
				}
				res, err := golden.Run(ctx, name, fx)
				if err != nil {
					logf("[FAIL] %s: %v\n", name, err)
					failed++
					continue
				}
				logf("[PASS] %s: %d/%d cases\n", name, res.Passed, res.Generated)
			}
			if failed > 0 {
				if out != nil {
					fmt.Fprintf(out, "conformance self-test: %d fixture(s) failed\n", failed)
				}
				return fmt.Errorf("conformance self-test: %d fixture(s) failed", failed)
			}
			logf("conformance self-test: all %d fixture(s) passed\n", len(fixtures))
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.goldenDir, "fixtures", "", "path to the golden fixtures directory (default: repo testdata/golden)")
	cmd.Flags().StringVar(&cfg.conformanceOut, "output", "", "write a per-fixture detail log to this file (also printed to stdout)")
	return cmd
}

func findGoldenDir() string {
	// Walk up from CWD looking for testdata/golden.
	wd, err := os.Getwd()
	if err != nil {
		return "testdata/golden"
	}
	for {
		candidate := filepath.Join(wd, "testdata", "golden")
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "testdata/golden"
}

// hasPlanSuffix reports whether a fixture basename is a generated .plan.json
// snapshot (which should not be treated as a source fixture).
func hasPlanSuffix(name string) bool {
	return strings.HasSuffix(name, ".plan.json")
}
