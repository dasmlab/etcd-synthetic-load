package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
)

func newReportCmd() *cobra.Command {
	var (
		runtimePath string
		planID      string
	)
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print the human-readable SMALL/MEDIUM/LARGE composition for a plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := config.LoadRuntime(runtimePath)
			if err != nil {
				return err
			}
			if planID == "" {
				plans, err := compose.List(rt.Paths.PlansDir)
				if err != nil {
					return err
				}
				if len(plans) == 0 {
					return fmt.Errorf("no plans in %s", rt.Paths.PlansDir)
				}
				planID = plans[0].Metadata.ID
			}
			plan, err := compose.Load(rt.Paths.PlansDir, planID)
			if err != nil {
				return err
			}
			fmt.Print(compose.FormatReport(plan))
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "path to runtime.yaml")
	cmd.Flags().StringVar(&planID, "plan", "", "plan id (default: newest)")
	return cmd
}

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run etcd load tests against a loaded target (not implemented yet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("test is not implemented yet — load a target first; harness lands in a later iteration")
		},
	}
	return cmd
}
