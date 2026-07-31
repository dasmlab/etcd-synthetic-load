package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/k8sclient"
	stat "github.com/dasmlab/etcd-synthetic-load/internal/status"
)

func newStatusCmd(gf *globalFlags) *cobra.Command {
	var profileID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report current synthetic namespaces/objects and approximate sizes",
		Long: `status scans the cluster for everything labeled
etcd-synthetic-load.dasmlab.org/managed=true (optionally scoped to a single
--profile-id) and reports per-namespace and total object counts plus an
approximate payload size (sum of Secret/ConfigMap data field lengths - not
the real etcd DB size; use etcd metrics for that).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := k8sclient.New(k8sclient.OptionsFromEnv(gf.kubeconfig))
			if err != nil {
				return fmt.Errorf("build kubernetes client: %w", err)
			}

			res, err := stat.Collect(context.Background(), client, profileID)
			if err != nil {
				return err
			}

			if res.TotalNamespaces == 0 {
				fmt.Println("No etcd-synthetic-load namespaces found.")
				return nil
			}

			fmt.Printf("%-24s %-8s %-10s %8s %8s %8s %12s\n", "NAMESPACE", "TIER", "PROFILE", "SECRETS", "HELM", "CONFIGM", "APPROX-MiB")
			for _, ns := range res.Namespaces {
				fmt.Printf("%-24s %-8s %-10s %8d %8d %8d %12.2f\n",
					ns.Name, ns.Tier, shorten(ns.ProfileID), ns.Secrets, ns.HelmSecrets, ns.ConfigMaps,
					float64(ns.ApproxBytes)/(1024*1024))
			}

			fmt.Println()
			fmt.Printf("TOTAL namespaces:  %d\n", res.TotalNamespaces)
			fmt.Printf("TOTAL secrets:     %d (helm-release style: %d)\n", res.TotalSecrets, res.TotalHelmSecrets)
			fmt.Printf("TOTAL configmaps:  %d\n", res.TotalConfigMaps)
			fmt.Printf("TOTAL approx size: %.2f GiB\n", res.TotalApproxGiB())
			return nil
		},
	}

	cmd.Flags().StringVar(&profileID, "profile-id", "", "only show objects belonging to this profile id (default: all)")
	return cmd
}

func shorten(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[:10]
}
