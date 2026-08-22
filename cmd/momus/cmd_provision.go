package main

import (
	"fmt"
	"os"

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
			planPath := args[0]
			raw, err := os.ReadFile(planPath)
			if err != nil {
				return fmt.Errorf("read test plan %s: %w", planPath, err)
			}
			_, dataset, err := decodeTestPlan(raw)
			if err != nil {
				return err
			}
			return provisionDataset(cfg, cmd.Context(), dataset)
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
