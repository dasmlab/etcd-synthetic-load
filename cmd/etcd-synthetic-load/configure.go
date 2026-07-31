package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dasmlab/etcd-synthetic-load/internal/config"
)

func newConfigureCmd() *cobra.Command {
	var (
		outPath     string
		displayName string
		apiServer   string
		dataDir     string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Write a starter runtime.yaml (static cluster/paths/pacing config)",
		Long: `configure writes a RuntimeConfig YAML. Credentials are NEVER written —
set OC_SERVER / OC_USER / OC_PASSWORD (or KUBECONFIG) in the environment.

WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := outPath
			if out == "" {
				out = os.Getenv("ESL_RUNTIME_CONFIG")
			}
			if out == "" {
				out = filepath.Join("data", "runtime.yaml")
			}
			if !force {
				if _, err := os.Stat(out); err == nil {
					return fmt.Errorf("%s already exists (use --force to overwrite)", out)
				}
			}
			rt := config.DefaultRuntime()
			rt.APIVersion = "etcd-synthetic-load.dasmlab.org/v1"
			rt.Kind = "RuntimeConfig"
			rt.Metadata.Name = "lab"
			rt.Metadata.Notes = "WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT"
			if displayName != "" {
				rt.Cluster.DisplayName = displayName
			} else if rt.Cluster.DisplayName == "" || rt.Cluster.DisplayName == "default" {
				rt.Cluster.DisplayName = "PROD-2"
			}
			if apiServer != "" {
				rt.Cluster.APIServer = apiServer
			}
			if dataDir != "" {
				rt.Paths.DataDir = dataDir
				rt.Paths.PlansDir = dataDir + "/plans"
				rt.Paths.RunsDir = dataDir + "/runs"
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			b, err := yaml.Marshal(rt)
			if err != nil {
				return err
			}
			header := "# RuntimeConfig — credentials are NOT stored here.\n# Use OC_SERVER / OC_USER / OC_PASSWORD or KUBECONFIG.\n# WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.\n\n"
			if err := os.WriteFile(out, append([]byte(header), b...), 0o644); err != nil {
				return err
			}
			fmt.Printf("Wrote %s\n", out)
			fmt.Println("Set credentials via env (.env / docker -e), never in this file.")
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&outPath, "out", "", "output path (default: data/runtime.yaml or ESL_RUNTIME_CONFIG)")
	f.StringVar(&displayName, "display-name", "", "cluster display name for UI")
	f.StringVar(&apiServer, "api-server", "", "OpenShift API URL (also overridable via OC_SERVER)")
	f.StringVar(&dataDir, "data-dir", "", "data directory for plans/runs")
	f.BoolVar(&force, "force", false, "overwrite existing file")
	return cmd
}
