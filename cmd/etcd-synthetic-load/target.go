package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
	"github.com/dasmlab/etcd-synthetic-load/internal/mapgen"
	"github.com/dasmlab/etcd-synthetic-load/internal/target"
)

func newTargetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Manage Targets (create / configure / generate / delete)",
		Long: `Target lifecycle for etcd-synthetic-load.

  create     → register cluster (no password in files)
  configure  → save Generation Seed (± tolerance validation)
  generate   → write sharded load map under data/targets/<id>/map/
  list|get   → inspect
  delete     → remove runtime record (run cleanup first for cluster objects)

WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.`,
	}
	cmd.AddCommand(newTargetCreateCmd())
	cmd.AddCommand(newTargetListCmd())
	cmd.AddCommand(newTargetGetCmd())
	cmd.AddCommand(newTargetConfigureCmd())
	cmd.AddCommand(newTargetGenerateCmd())
	cmd.AddCommand(newTargetDeleteCmd())
	return cmd
}

func targetStoreFromFlags(runtimePath string) (*target.Store, *config.RuntimeConfig, error) {
	rt, err := config.LoadRuntime(runtimePath)
	if err != nil {
		return nil, nil, err
	}
	if err := config.EnsureDirs(rt); err != nil {
		return nil, nil, err
	}
	st, err := target.NewStore(rt.Paths.DataDir)
	return st, rt, err
}

func newTargetCreateCmd() *cobra.Command {
	var runtimePath, name, api, user string
	var tol float64
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Target (cluster under test)",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			if api == "" {
				api = os.Getenv("OC_SERVER")
			}
			if user == "" {
				user = os.Getenv("OC_USER")
			}
			t, err := st.Create(name, api, user, tol)
			if err != nil {
				return err
			}
			fmt.Printf("Created target %s (%s)\n", t.Metadata.ID, t.Spec.APIServer)
			fmt.Println("Set OC_PASSWORD (or KUBECONFIG) in the environment — never in target.yaml.")
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	cmd.Flags().StringVar(&name, "name", "PROD-2", "display name")
	cmd.Flags().StringVar(&api, "api-server", "", "OpenShift API URL")
	cmd.Flags().StringVar(&user, "username", "", "login username hint")
	cmd.Flags().Float64Var(&tol, "tolerance-percent", 10, "±% allowed on seed budget vs utilization")
	return cmd
}

func newTargetListCmd() *cobra.Command {
	var runtimePath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			list, err := st.List()
			if err != nil {
				return err
			}
			for _, t := range list {
				fmt.Printf("%-28s  %-12s  %s\n", t.Metadata.ID, t.Status.Phase, t.Spec.APIServer)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	return cmd
}

func newTargetGetCmd() *cobra.Command {
	var runtimePath, id string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show a Target",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			t, err := st.Get(id)
			if err != nil {
				return err
			}
			b, _ := os.ReadFile(st.Dir(id) + "/target.yaml")
			fmt.Print(string(b))
			_ = t
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	cmd.Flags().StringVar(&id, "id", "", "target id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTargetConfigureCmd() *cobra.Command {
	var runtimePath, id, seedPath string
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Save Generation Seed for a Target (validates ± tolerance)",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			var seed compose.SeedConfig
			if seedPath != "" {
				b, err := os.ReadFile(seedPath)
				if err != nil {
					return err
				}
				if err := yaml.Unmarshal(b, &seed); err != nil {
					return err
				}
			} else {
				seed = compose.DefaultSeed(5.0)
			}
			if err := st.SaveSeed(id, seed); err != nil {
				return err
			}
			fmt.Printf("Configured target %s (seed validated)\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	cmd.Flags().StringVar(&id, "id", "", "target id")
	cmd.Flags().StringVar(&seedPath, "seed", "", "path to seed.yaml (default: built-in DefaultSeed)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTargetGenerateCmd() *cobra.Command {
	var runtimePath, id string
	var perShard int
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate sharded load map for a configured Target",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			seed, err := st.LoadSeed(id)
			if err != nil {
				return fmt.Errorf("configure the target first: %w", err)
			}
			man, err := mapgen.Generate(id, st.Dir(id), *seed, perShard)
			if err != nil {
				_ = st.SetPhase(id, target.StatusConfigured, err.Error())
				return err
			}
			if err := st.SetPhase(id, target.StatusGenerated, man.Validation.Message); err != nil {
				return err
			}
			fmt.Printf("Generated map for %s: %d shards, %d objects, %.3f GiB (est)\n",
				id, man.Summary.TotalShards, man.Summary.TotalObjects, man.Summary.TotalSizeGiB)
			fmt.Printf("  manifest: %s/map/manifest.yaml\n", st.Dir(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	cmd.Flags().StringVar(&id, "id", "", "target id")
	cmd.Flags().IntVar(&perShard, "objects-per-shard", mapgen.DefaultObjectsPerShard, "objects per shard file")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTargetDeleteCmd() *cobra.Command {
	var runtimePath, id string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete Target runtime record (run cleanup first for cluster objects)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing without --yes (run 'cleanup' first if the cluster was loaded)")
			}
			st, _, err := targetStoreFromFlags(runtimePath)
			if err != nil {
				return err
			}
			if err := st.Delete(id); err != nil {
				return err
			}
			fmt.Printf("Deleted target %s from runtime data\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimePath, "runtime", "", "runtime.yaml path")
	cmd.Flags().StringVar(&id, "id", "", "target id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm delete")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
