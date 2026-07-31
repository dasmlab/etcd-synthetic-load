package main

import (
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

type globalFlags struct {
	kubeconfig string
}

func newRootCmd() *cobra.Command {
	gf := &globalFlags{}

	root := &cobra.Command{
		Use:   "etcd-synthetic-load",
		Short: "Synthetic etcd load generator for OpenShift/Kubernetes triage testing",
		Long: banner + `
etcd-synthetic-load simulates a client-like etcd usage profile (lots of
Secrets, ConfigMaps, and Helm-release-style secrets spread across
Small/Medium/Large namespaces) so that etcd triage/monitoring scripts can be
exercised against realistic object volumes and payload sizes without waiting
for a real workload to accumulate that state organically.

Typical flow:

  1. etcd-synthetic-load plan   --target-gib 5.6 --target-secrets 120000 --target-configmaps 80000
  2. etcd-synthetic-load load   --dry-run
  3. etcd-synthetic-load load   --i-understand-this-stresses-etcd
  4. etcd-synthetic-load status
  5. etcd-synthetic-load cleanup

Cluster auth (checked in this order):
  1. --kubeconfig <path>
  2. $KUBECONFIG
  3. $OC_SERVER + $OC_USER + $OC_PASSWORD  (shells out to 'oc login')
  4. in-cluster config (when running as a Pod)
  5. ~/.kube/config
`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&gf.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then OC_SERVER/OC_USER/OC_PASSWORD, then in-cluster, then ~/.kube/config)")

	root.AddCommand(newPlanCmd())
	root.AddCommand(newLoadCmd(gf))
	root.AddCommand(newStatusCmd(gf))
	root.AddCommand(newCleanupCmd(gf))

	return root
}
