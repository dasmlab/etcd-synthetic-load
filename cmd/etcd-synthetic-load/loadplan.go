package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
	"github.com/dasmlab/etcd-synthetic-load/internal/k8sclient"
	"github.com/dasmlab/etcd-synthetic-load/internal/loadgen"
)

func newLoadPlanCmd(gf *globalFlags) *cobra.Command {
	var (
		runtimePath string
		planID      string
		dryRun      bool
		understand  bool
	)

	cmd := &cobra.Command{
		Use:   "load-plan",
		Short: "Apply a generated LoadPlan with controlled batching (preferred over legacy 'load')",
		Long: `load-plan reads a plan from data/plans and creates objects with paced batches
(concurrency + pauses from runtime.yaml). This is intentionally not a dumb hammer.

Real (non-dry-run) loads require --i-understand-this-stresses-etcd.

WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := config.LoadRuntime(runtimePath)
			if err != nil {
				return err
			}
			if planID == "" {
				return fmt.Errorf("--plan is required (plan id or path)")
			}
			plan, err := compose.Load(rt.Paths.PlansDir, planID)
			if err != nil {
				return err
			}

			opts := loadgen.PacedOptions{
				Plan:    plan,
				Runtime: rt,
				DryRun:  dryRun,
				Confirm: understand,
				ProgressFn: func(done, total int64, message string) {
					fmt.Fprintf(os.Stderr, "\r[%d/%d] %s", done, total, message)
				},
			}

			if !dryRun {
				cfg, err := k8sclient.BuildConfig(k8sclient.OptionsFromEnv(gf.kubeconfig))
				if err != nil {
					return err
				}
				client, err := kubernetes.NewForConfig(cfg)
				if err != nil {
					return err
				}
				dyn, _ := dynamic.NewForConfig(cfg)
				opts.Client = client
				opts.Dynamic = dyn
			}

			ctx := context.Background()
			start := time.Now()
			res, err := loadgen.RunPaced(ctx, opts)
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return err
			}
			fmt.Printf("Load finished in %s (dryRun=%v)\n", time.Since(start).Round(time.Second), dryRun)
			fmt.Printf("  namespaces created: %d\n", res.NamespacesCreated)
			for k, v := range res.Created {
				fmt.Printf("  created %s: %d\n", k, v)
			}
			for k, v := range res.Existing {
				fmt.Printf("  existing %s: %d\n", k, v)
			}
			for k, v := range res.Skipped {
				fmt.Printf("  skipped %s: %d\n", k, v)
			}
			if len(res.Errors) > 0 {
				fmt.Printf("  errors: %d (showing up to 10)\n", len(res.Errors))
				for i, e := range res.Errors {
					if i >= 10 {
						break
					}
					fmt.Printf("    - %s\n", e)
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&runtimePath, "runtime", "", "path to runtime.yaml")
	f.StringVar(&planID, "plan", "", "plan id or path under plansDir")
	f.BoolVar(&dryRun, "dry-run", false, "print what would be created without touching the cluster")
	f.BoolVar(&understand, "i-understand-this-stresses-etcd", false, "required for a real (non-dry-run) load")
	return cmd
}
