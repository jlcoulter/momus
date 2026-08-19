package main

import (
	"fmt"
	"os"

	provisioning "github.com/jlcoulter/momus/internal/fhir/provisioning"
	"github.com/spf13/cobra"
)

// newProvisionCmd returns the "coverage provision" command, whose single role
// is stage J execution: upload the seed dataset carried by a generated test
// plan to the target server, ahead of any test execution. It consumes the test
// plan (from "coverage ast"), not the package, so provisioning is driven by
// exactly the data the generated tests reference.
func newProvisionCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision <path-to-test-plan.json>",
		Short: "Upload the seed dataset from a test plan to the target server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg.baseURL == "" {
				return fmt.Errorf("base URL is required; provide --base-url")
			}
			// Resolve the write base URL up front: the provisioner needs a concrete
			// URL (it does not default internally).
			writeBase := cfg.writeBaseURL
			if writeBase == "" {
				writeBase = cfg.baseURL
			}
			writeBasicUser := cfg.writeBasicUsername
			if writeBasicUser == "" {
				writeBasicUser = cfg.apiBasicUsername
			}
			writeBasicPass := cfg.writeBasicPassword
			if writeBasicPass == "" {
				writeBasicPass = cfg.apiBasicPassword
			}

			planPath := args[0]
			raw, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("read test plan %s: %w", planPath, err)
			}
			_, dataset, err := decodeTestPlan(raw)
			if err != nil {
				return err
			}
			if dataset == nil || len(dataset.Resources) == 0 {
				fmt.Printf("Provisioning skipped: test plan carries no seed resources\n")
				return nil
			}

			provisioner := provisioning.New(writeBase, &provisioning.Options{
				BearerToken:   cfg.apiBearerToken,
				BasicUsername: writeBasicUser,
				BasicPassword: writeBasicPass,
				Tracer:        newDebugTracer(cfg.debug),
			})
			fmt.Printf("Provisioning phase: uploading %d seed resources to %s\n", len(dataset.Resources), writeBase)
			seed := provisioner.ProvisionAll(cmd.Context(), dataset)
			if !seed.Complete() {
				fmt.Printf("WARNING: dataset seeding incomplete — %d of %d resources uploaded. Data seeding is essential to achieve full coverage success. Fix the failing resources and re-run.\n", seed.Provisioned, seed.Provisioned+seed.Failed)
				for _, failure := range seed.Failures {
					fmt.Printf("  - %s\n", failure.Describe())
				}
				if !cfg.debug {
					fmt.Printf("Run with --debug to write the rejected payloads and full server responses to %s for inspection.\n", debugOutputDir)
				}
				if err := writeDebugProvisionFailures(cfg.debug, seed.Failures); err != nil {
					return err
				}
				// Incomplete provisioning is a warning, not a failure: the run can still
				// proceed (and other commands rely on this command exiting successfully
				// when run as part of a pipeline). Failures are reported above so the
				// operator can fix and re-run.
				return nil
			}
			fmt.Printf("Provisioning complete: %d resources uploaded\n", seed.Provisioned)
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.baseURL, "base-url", "", "target FHIR base URL for resource creation")
	cmd.Flags().StringVar(&cfg.writeBaseURL, "write-base-url", "", "alternate FHIR base URL for resource creation (write) requests; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.apiBearerToken, "api-bearer-token", "", "bearer token used for API requests during provisioning")
	cmd.Flags().StringVar(&cfg.apiBasicUsername, "api-basic-username", "", "basic auth username used for API requests during provisioning")
	cmd.Flags().StringVar(&cfg.apiBasicPassword, "api-basic-password", "", "basic auth password used for API requests during provisioning")
	cmd.Flags().StringVar(&cfg.writeBasicUsername, "write-basic-username", "", "basic auth username used for write requests to --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.writeBasicPassword, "write-basic-password", "", "basic auth password used for write requests to --write-base-url; defaults to --api-basic-password")
	return cmd
}
