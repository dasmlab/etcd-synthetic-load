package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
)

func newGenerateCmd() *cobra.Command {
	var (
		runtimePath string
		targetPath  string
		name        string
		utilGiB     float64
		utilPct     float64
		quotaGiB    float64
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a Small/Medium/Large load plan and write it under data/plans",
		Long: `generate builds a t-shirt composition (SMALL/MEDIUM/LARGE) that sums toward
the utilization target (default 5.6 GiB). Object totals omitted from the target
YAML (or CLI) use built-in defaults; SmallX/LargeX ranges are filled automatically.

Nothing is applied to a cluster — use 'load' for that.

WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := config.LoadRuntime(runtimePath)
			if err != nil {
				return err
			}
			if err := config.EnsureDirs(rt); err != nil {
				return err
			}

			in := compose.GenerateInput{
				Name:               name,
				ClusterDisplayName: rt.Cluster.DisplayName,
				AssumedQuotaGiB:    quotaGiB,
				Objects:            map[compose.ObjectKind]int{},
			}
			if targetPath != "" {
				t, err := config.LoadTargetFile(targetPath)
				if err != nil {
					return err
				}
				in = compose.FromLoadTarget(t, rt.Cluster.DisplayName)
				if name != "" {
					in.Name = name
				}
			}
			if cmd.Flags().Changed("target-gib") {
				v := utilGiB
				in.UtilizationGiB = &v
				in.UtilizationPercent = nil
			}
			if cmd.Flags().Changed("target-percent") {
				v := utilPct
				in.UtilizationPercent = &v
				in.UtilizationGiB = nil
			}

			plan, err := compose.Generate(in)
			if err != nil {
				return err
			}
			path, err := compose.Save(plan, rt.Paths.PlansDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Wrote %s\n\n", path)
			fmt.Print(compose.FormatReport(plan))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&runtimePath, "runtime", "", "path to runtime.yaml (or ESL_RUNTIME_CONFIG)")
	f.StringVar(&targetPath, "target", "", "path to LoadTarget YAML (optional; defaults when omitted)")
	f.StringVar(&name, "name", "generated", "plan metadata name")
	f.Float64Var(&utilGiB, "target-gib", 5.6, "utilization target in GiB")
	f.Float64Var(&utilPct, "target-percent", 0, "utilization as % of assumed quota")
	f.Float64Var(&quotaGiB, "assumed-quota-gib", 8.0, "assumed etcd quota GiB when using --target-percent")
	return cmd
}
