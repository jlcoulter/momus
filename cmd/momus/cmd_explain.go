package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlcoulter/momus/internal/home"
	"github.com/spf13/cobra"
)

// newExplainCmd renders one failing case in full depth from a run's output
// directory, so a developer can inspect a single case's request, response,
// assertion, and trace without wading through the whole report. The case is
// located by requirement id (e.g. "search|Patient|name|search-valid") or by
// its file name under <dir>/cases/.
func newExplainCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <requirement-id>",
		Short: "Render one case in full depth from an output directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := cfg.OutputDir
			if dir == "" {
				dir = home.OutputDir()
			}
			id := args[0]
			file := filepath.Join(dir, "cases", caseFileNameForID(id))
			raw, err := os.ReadFile(file)
			if err != nil {
				// Fall back to treating the arg as a file name.
				file = filepath.Join(dir, "cases", id)
				raw, err = os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("case %q not found in %s/cases: %w", id, dir, err)
				}
			}

			var caseObj struct {
				RequirementID string `json:"requirementId"`
				Passed        bool   `json:"passed"`
				Requirement   struct {
					HumanID     string `json:"humanId"`
					Description string `json:"description"`
					Domain      string `json:"domain"`
					Variant     string `json:"variant"`
				} `json:"requirement"`
			}
			if err := json.Unmarshal(raw, &caseObj); err != nil {
				return fmt.Errorf("decode case %s: %w", file, err)
			}

			label := caseObj.Requirement.HumanID
			if label == "" {
				label = caseObj.RequirementID
			}
			status := "PASS"
			if !caseObj.Passed {
				status = "FAIL"
			}
			fmt.Printf("case %s [%s]\n", label, status)
			if caseObj.Requirement.Description != "" {
				fmt.Printf("what is tested: %s\n", caseObj.Requirement.Description)
			}
			if caseObj.Requirement.Domain != "" || caseObj.Requirement.Variant != "" {
				fmt.Printf("domain/variant: %s/%s\n", caseObj.Requirement.Domain, caseObj.Requirement.Variant)
			}
			var pretty map[string]any
			if err := json.Unmarshal(raw, &pretty); err != nil {
				return fmt.Errorf("decode case %s: %w", file, err)
			}
			out, err := json.MarshalIndent(pretty, "", "  ")
			if err != nil {
				return err
			}
			fmt.Printf("full detail (from %s):\n%s\n", file, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.OutputDir, "dir", "", "output directory to read cases from (default: $HOME/.momus/output)")
	return cmd
}

// caseFileNameForID mirrors report.caseFileName's sanitisation so explain can
// locate a case by requirement id.
func caseFileNameForID(id string) string {
	safe := strings.NewReplacer("|", "_", " ", "_", "/", "_", ":", "_", "{", "_", "}", "_").Replace(id)
	return safe + ".json"
}
