package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	karateexport "github.com/jlcoulter/momus/internal/core/karate"
	"github.com/spf13/cobra"
)

// karateConfigTemplate is a generated karate-config.js that parameterizes the
// base URLs the exported .feature files reference. baseUrl is the read/search
// base and writeBaseUrl the write base; both default to the same value and can
// be overridden via -D system properties at Karate run time.
const karateConfigTemplate = `function fn() {
  var config = {
    baseUrl: 'http://localhost:8080/fhir',
    writeBaseUrl: 'http://localhost:8080/fhir'
  };
  if (karate.properties['momus.baseUrl']) {
    config.baseUrl = karate.properties['momus.baseUrl'];
  }
  if (karate.properties['momus.writeBaseUrl']) {
    config.writeBaseUrl = karate.properties['momus.writeBaseUrl'];
  }
  return config;
}
`

// newKarateCmd returns the "coverage karate" command, which exports a generated
// test plan (from "coverage ast" / "coverage plan") as Karate .feature files,
// one per resource type. Seed data is embedded in each feature's Background
// block so the setup sits near the tests that reference it, and each scenario
// carries @requirement/@domain/@variant tags for traceability.
func newKarateCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "karate <path-to-test-plan.json>",
		Short: "Export a test plan as Karate .feature files (one per resource type)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read test plan %s: %w", args[0], err)
			}
			astPlan, _, err := decodeTestPlan(raw)
			if err != nil {
				return err
			}

			files, err := karateexport.Export(astPlan, karateexport.Options{})
			if err != nil {
				return fmt.Errorf("export Karate: %w", err)
			}
			if len(files) == 0 {
				return fmt.Errorf("no resource types found in plan %s", args[0])
			}

			outDir := cfg.KarateOutDir
			if outDir == "" {
				outDir = "./karate-features"
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create output dir %s: %w", outDir, err)
			}

			rendered := karateexport.RenderAll(files)
			names := make([]string, 0, len(rendered))
			for name := range rendered {
				names = append(names, name)
			}
			sort.Strings(names)

			totalScenarios := 0
			for _, name := range names {
				if err := os.WriteFile(filepath.Join(outDir, name), []byte(rendered[name]), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", name, err)
				}
				fmt.Printf("wrote %s\n", filepath.Join(outDir, name))
				for _, f := range files {
					if f.Name+".feature" == name {
						totalScenarios += len(f.Scenarios)
					}
				}
			}

			if cfg.GenerateKarateCfg {
				cfgPath := filepath.Join(outDir, "karate-config.js")
				if err := os.WriteFile(cfgPath, []byte(karateConfigTemplate), 0o644); err != nil {
					return fmt.Errorf("write %s: %w", cfgPath, err)
				}
				fmt.Printf("wrote %s\n", cfgPath)
			}

			fmt.Printf("Exported %d feature files with %d scenarios to %s\n", len(files), totalScenarios, outDir)
			if !cfg.GenerateKarateCfg {
				fmt.Println("Set baseUrl/writeBaseUrl in karate-config.js (or pass --karate-config to generate one)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.KarateOutDir, "output-dir", "./karate-features", "directory to write .feature files to")
	cmd.Flags().BoolVar(&cfg.GenerateKarateCfg, "karate-config", false, "also write a karate-config.js template into the output directory")
	return cmd
}
