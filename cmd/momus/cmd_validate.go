package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jlcoulter/momus/internal/fhir/validate"
	"github.com/spf13/cobra"
)

// newValidateCmd returns the "validate" command: validate a FHIR JSON resource
// against a profile (or against all profiles registered for its resource type
// from the loaded packages), printing one line per issue. It is the standalone
// front-end for internal/fhir/validate.
func newValidateCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <resource.json>",
		Short: "Validate a FHIR resource against a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read resource %s: %w", args[0], err)
			}
			var resource map[string]any
			if err := json.Unmarshal(raw, &resource); err != nil {
				return fmt.Errorf("resource %s is not valid JSON: %w", args[0], err)
			}

			// Build the registry from the configured package(s).
			reg, err := buildRegistryForValidate(cfg, args[0])
			if err != nil {
				return err
			}

			v := validate.New(reg)
			profileURLs := profileURLsFor(cfg, resource)
			var allIssues []validate.Issue
			for _, url := range profileURLs {
				issues, err := v.Validate(cmd.Context(), url, resource)
				if err != nil {
					fmt.Fprintf(os.Stderr, "validate: skipping profile %s: %v\n", url, err)
					continue
				}
				allIssues = append(allIssues, issues...)
			}
			if len(allIssues) == 0 {
				fmt.Printf("OK: resource conforms to %d profile(s)\n", len(profileURLs))
				return nil
			}
			for _, iss := range allIssues {
				fmt.Printf("%s: %s: %s\n", iss.Path, iss.Kind, iss.Message)
			}
			return fmt.Errorf("resource failed validation with %d issue(s)", len(allIssues))
		},
	}
	cmd.Flags().StringSliceVar(&cfg.ProfileURLs, "profile", nil, "profile URL(s) to validate against (repeatable; when empty uses the resource's meta.profile)")
	cmd.Flags().StringVar(&cfg.PackagePath, "package", "", "FHIR package archive (.tgz) to load profiles from")
	return cmd
}
