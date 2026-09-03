package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	testbulk "github.com/jlcoulter/momus/internal/fhir/bulk"
	fhirpackage "github.com/jlcoulter/momus/internal/fhir/package"
	"github.com/jlcoulter/momus/internal/fhir/provisioning"
	"github.com/jlcoulter/momus/internal/home"
	"github.com/spf13/cobra"
)

// newBulkCmd returns the "coverage bulk" command.
func newBulkCmd(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk <path-to-package.tgz>",
		Short: "Generate NDJSON bulk data from derived coverage requirements",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rootPath := args[0]
			cacheDir := cfg.DownloadDir
			if cacheDir == "" {
				cacheDir = home.PackageCacheDir()
			}

			graph, err := fhirpackage.ResolveLocalPackageGraphWithOptions(rootPath, fhirpackage.ResolveOptions{
				DepsDir:        cfg.DepsDir,
				DownloadDir:    cacheDir,
				ConflictPolicy: fhirpackage.ConflictPolicy(cfg.ConflictPolicy),
			})
			if err != nil {
				return err
			}
			if err := writeDebugGraph(cfg.Debug, graph); err != nil {
				return err
			}

			builder := fhirpackage.NewRegistryBuilder()
			reg, err := builder.BuildFromPackagesScoped(graph.Packages, graph.Root)
			if err != nil {
				return err
			}

			resourceTypes := cfg.IncludeResourceTypes
			if len(resourceTypes) == 0 {
				seen := make(map[string]bool)
				for _, sd := range reg.ScopedStructureDefinitions() {
					if sd.Type == "" || sd.Kind != "resource" || abstractResourceTypes[sd.Type] {
						continue
					}
					if !seen[sd.Type] {
						seen[sd.Type] = true
						resourceTypes = append(resourceTypes, sd.Type)
					}
				}
				sort.Strings(resourceTypes)
			}

			if cfg.BulkCount < 0 {
				return fmt.Errorf("--count must be non-negative, got %d", cfg.BulkCount)
			}

			corpusGenerator := testbulk.NewCorpusGenerator(reg, cfg.Exhaustive)

			var out io.Writer = os.Stdout
			if cfg.BaseURL != "" {
				// When provisioning directly to a server, the NDJSON stream is
				// discarded rather than written to a file, even if --output was
				// specified: the resources are uploaded, not persisted locally.
				out = io.Discard
			}
			var f *os.File
			if cfg.OutputPath != "" && cfg.BaseURL == "" {
				outputPath, err := resolveBulkOutputPath(cfg.OutputPath)
				if err != nil {
					return err
				}
				if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("create bulk output dir %s: %w", dir, err)
					}
				}
				f, err = os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("create bulk file %s: %w", outputPath, err)
				}
				defer f.Close()
				out = f
				cfg.OutputPath = outputPath
			}

			w := testbulk.NewWriter(out)
			// Provisioner is created once and reused for every batch so the
			// streaming pipeline provisions each mixed-type web as soon as it is
			// generated, rather than holding the whole corpus in memory.
			var provisioner *provisioning.ServerProvisioner
			if cfg.BaseURL != "" {
				provisioner = newBulkProvisioner(cfg)
			}
			// Debug bulk output is streamed per-batch so a very large corpus
			// never accumulates every resource body in memory just for the
			// debug dump.
			debugBulk, err := newDebugBulkWriter(cfg.Debug)
			if err != nil {
				return err
			}
			var provisioned, failed int
			var generated int
			var failures []provisioning.Failure

			batchSize := cfg.BulkBatchSize
			if batchSize < 1 {
				batchSize = 1
			}
			pipelineDepth := cfg.BulkPipelineDepth
			if pipelineDepth < 1 {
				pipelineDepth = 1
			}
			start := time.Now()
			batches, errs := corpusGenerator.GenerateCorpusStreamed(cmd.Context(), resourceTypes, cfg.BulkCount, parsePerTypeCounts(cfg.BulkPerTypeCounts), batchSize, pipelineDepth)
			for batch := range batches {
				// Provision this batch immediately: its references only point to
				// resources already emitted (and thus already on the server).
				if provisioner != nil {
					res := provisioner.ProvisionBatch(cmd.Context(), batch.Instances)
					provisioned += res.Provisioned
					failed += res.Failed
					failures = append(failures, res.Failures...)
				}
				// Write the batch to NDJSON. Finalization batches re-emit
				// instances that were already written in their initial batch, so
				// they are provisioned (a PUT updates the server) but not
				// re-written to the NDJSON stream.
				if !batch.Finalize {
					if err := w.WriteInstances(batch.Instances); err != nil {
						return err
					}
					generated += len(batch.Instances)
					if debugBulk != nil {
						if err := debugBulk.WriteInstances(batch.Instances); err != nil {
							return err
						}
					}
				}
			}
			if err := <-errs; err != nil {
				return err
			}
			if debugBulk != nil {
				if err := debugBulk.Close(); err != nil {
					return err
				}
			}
			if err := w.Close(); err != nil {
				return err
			}
			if provisioner != nil {
				if failed > 0 {
					fmt.Printf("WARNING: bulk repository stream incomplete: %d of %d resources uploaded\n", provisioned, provisioned+failed)
					for _, failure := range failures {
						fmt.Printf("  - %s\n", failure.Describe())
					}
					if !cfg.Debug {
						fmt.Printf("Run with --debug to write the rejected payloads and full server responses to %s for inspection.\n", debugOutputDir)
					}
					if err := writeDebugProvisionFailures(cfg.Debug, failures); err != nil {
						return err
					}
					return fmt.Errorf("bulk repository stream incomplete: %d of %d resources uploaded", provisioned, provisioned+failed)
				}
				fmt.Printf("Bulk repository stream complete: %d resources uploaded\n", provisioned)
				elapsed := time.Since(start)
				if secs := elapsed.Seconds(); secs > 0 {
					fmt.Printf("  %.0f resources/sec (provisioning)\n", float64(provisioned)/secs)
				}
			}
			fmt.Printf("Generated NDJSON bulk data: %d resources across %d resource types\n", generated, len(resourceTypes))
			elapsed := time.Since(start)
			if secs := elapsed.Seconds(); secs > 0 {
				fmt.Printf("  %.0f resources/sec (generation)\n", float64(generated)/secs)
			}
			if cfg.OutputPath != "" && cfg.BaseURL == "" {
				fmt.Printf("Bulk data written to %s\n", cfg.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.DepsDir, "deps-dir", "", "directory to search for dependency package archives (.tgz/.tar.gz)")
	cmd.Flags().StringVar(&cfg.DownloadDir, "download-dir", "", "directory to store downloaded dependency package archives")
	cmd.Flags().StringVar(&cfg.ConflictPolicy, "conflict-policy", string(fhirpackage.ConflictPolicyRootWins), "dependency conflict policy: root-wins or strict")
	cmd.Flags().StringVar(&cfg.OutputPath, "output", "", "write NDJSON bulk data to a file")
	cmd.Flags().BoolVar(&cfg.Exhaustive, "exhaustive", true, "populate optional elements to produce fuller, more complete resources")
	cmd.Flags().IntVar(&cfg.BulkCount, "count", 25, "number of resources to generate per resource type")
	cmd.Flags().IntVar(&cfg.BulkBatchSize, "batch-size", 100, "number of resource webs to emit per streaming batch; bounds peak memory")
	cmd.Flags().IntVar(&cfg.BulkPipelineDepth, "pipeline-depth", 4, "buffered batches generated ahead of provisioning to overlap synthesis and upload")
	cmd.Flags().IntVar(&cfg.Concurrency, "concurrency", 8, "maximum concurrent HTTP requests to the repository (<=0 = unlimited)")
	cmd.Flags().StringSliceVar(&cfg.BulkPerTypeCounts, "per-type", nil, "per-type resource counts as Type=Count (repeatable); overrides --count")
	cmd.Flags().StringSliceVar(&cfg.IncludeResourceTypes, "include-resource", nil, "include only these resource types (repeatable); referenced target types are added automatically")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "target FHIR repository base URL for streaming generated resources")
	cmd.Flags().StringVar(&cfg.WriteBaseURL, "write-base-url", "", "alternate FHIR repository base URL for write requests; defaults to --base-url")
	cmd.Flags().StringVar(&cfg.ApiBearerToken, "api-bearer-token", "", "bearer token used for repository write requests")
	cmd.Flags().StringVar(&cfg.ApiBasicUsername, "api-basic-username", "", "basic auth username used for repository write requests")
	cmd.Flags().StringVar(&cfg.ApiBasicPassword, "api-basic-password", "", "basic auth password used for repository write requests")
	cmd.Flags().StringVar(&cfg.WriteBasicUsername, "write-basic-username", "", "basic auth username used for --write-base-url; defaults to --api-basic-username")
	cmd.Flags().StringVar(&cfg.WriteBasicPassword, "write-basic-password", "", "basic auth password used for --write-base-url; defaults to --api-basic-password")
	return cmd
}

// newBulkProvisioner builds a ServerProvisioner for streaming generated
// resources to the configured repository, reused across every corpus batch.
func newBulkProvisioner(cfg *config) *provisioning.ServerProvisioner {
	writeBase := cfg.WriteBaseURL
	if writeBase == "" {
		writeBase = cfg.BaseURL
	}
	writeBasicUser := cfg.WriteBasicUsername
	if writeBasicUser == "" {
		writeBasicUser = cfg.ApiBasicUsername
	}
	writeBasicPass := cfg.WriteBasicPassword
	if writeBasicPass == "" {
		writeBasicPass = cfg.ApiBasicPassword
	}
	concurrency := cfg.Concurrency
	if concurrency < 1 {
		concurrency = 16
	}
	// The default http.Transport keeps only 2 idle connections per host. Bulk
	// provisioning opens up to --concurrency PUTs at once, so 16 goroutines
	// would otherwise thrash on 2 connections: every extra request pays for a
	// fresh TCP connect/TLS handshake. Size the idle pool to the concurrency
	// (plus headroom) so connections are reused and keep-alive actually kicks
	// in. The blanket client timeout is dropped: cancellation is driven by the
	// request context, and a hard deadline on the whole body read would kill
	// connections that could otherwise be reused under a slow server.
	transport := &http.Transport{
		MaxIdleConns:        concurrency + 64,
		MaxIdleConnsPerHost: concurrency + 16,
		IdleConnTimeout:     90 * time.Second,
		MaxConnsPerHost:     concurrency * 2,
	}
	client := &http.Client{Transport: transport}
	return provisioning.New(writeBase, &provisioning.Options{
		BearerToken:   cfg.ApiBearerToken,
		BasicUsername: writeBasicUser,
		BasicPassword: writeBasicPass,
		Tracer:        newDebugTracer(cfg.Debug),
		Concurrency:   concurrency,
		HTTPClient:    client,
	})
}

func resolveBulkOutputPath(outputPath string) (string, error) {
	if outputPath == "" {
		return "", nil
	}
	if strings.HasSuffix(outputPath, string(os.PathSeparator)) {
		return filepath.Join(outputPath, "bulk.ndjson"), nil
	}
	info, err := os.Stat(outputPath)
	if err == nil && info.IsDir() {
		return filepath.Join(outputPath, "bulk.ndjson"), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat bulk output path %s: %w", outputPath, err)
	}
	return outputPath, nil
}
