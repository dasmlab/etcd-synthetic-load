package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/k8sclient"
	"github.com/dasmlab/etcd-synthetic-load/internal/loadgen"
	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

func newLoadCmd(gf *globalFlags) *cobra.Command {
	var profilePath string
	var dryRun bool
	var concurrency int
	var understand bool

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Apply a profile.yaml to the cluster (creates namespaces, Secrets, ConfigMaps)",
		Long: banner + `
load reads a profile.yaml (from 'plan') and creates the described
namespaces, Secrets, and ConfigMaps on the target cluster. It is idempotent:
re-running load will skip objects that already exist (AlreadyExists is not
treated as an error), so you can resume an interrupted run or "top up"
toward the profile's targets.

Every object is labeled with etcd-synthetic-load.dasmlab.org/managed=true
and a profile-id, so 'status' and 'cleanup' can find (and only find) what
this tool created.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadgen.EnsureNoDangerousDefaults(understand, dryRun); err != nil {
				return err
			}
			if !dryRun {
				fmt.Print(banner)
			}

			p, err := profile.Load(profilePath)
			if err != nil {
				return fmt.Errorf("load profile: %w (run 'plan' first, or pass --profile)", err)
			}

			opts := loadgen.Options{
				Profile:     p,
				DryRun:      dryRun,
				Concurrency: concurrency,
				ProgressFn: func(done, total int64) {
					fmt.Printf("  progress: %d/%d objects\n", done, total)
				},
			}

			if !dryRun {
				c, err := k8sclient.New(k8sclient.OptionsFromEnv(gf.kubeconfig))
				if err != nil {
					return fmt.Errorf("build kubernetes client: %w", err)
				}
				opts.Client = c
			}

			fmt.Printf("Applying profile %s (dry-run=%v, concurrency=%d)\n", p.Metadata.ID, dryRun, concurrency)
			res, err := loadgen.Run(context.Background(), opts)
			if err != nil {
				return err
			}

			label := "Created"
			if dryRun {
				label = "Would create"
			}
			fmt.Println()
			fmt.Printf("%s namespaces: %d (existing: %d)\n", label, res.NamespacesCreated, res.NamespacesExisting)
			fmt.Printf("%s secrets:    %d (existing: %d)\n", label, res.SecretsCreated, res.SecretsExisting)
			fmt.Printf("%s configmaps: %d (existing: %d)\n", label, res.ConfigMapsCreated, res.ConfigMapsExisting)
			if len(res.Errors) > 0 {
				fmt.Printf("\n%d error(s) occurred (showing up to 20):\n", len(res.Errors))
				for i, e := range res.Errors {
					if i >= 20 {
						break
					}
					fmt.Printf("  - %v\n", e)
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&profilePath, "profile", "profile.yaml", "path to profile.yaml (from 'plan')")
	f.BoolVar(&dryRun, "dry-run", false, "compute and print planned counts without contacting the cluster")
	f.IntVar(&concurrency, "concurrency", 20, "number of concurrent object-creation workers")
	f.BoolVar(&understand, "i-understand-this-stresses-etcd", false, "required to run a real (non-dry-run) load; acknowledges this tool intentionally stresses etcd")

	return cmd
}
