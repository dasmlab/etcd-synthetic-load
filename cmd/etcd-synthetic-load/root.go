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
etcd-synthetic-load builds Small/Medium/Large synthetic compositions and can
apply them in a controlled (batched) way so triage tooling can be exercised
against realistic object volumes.

Typical flow (CLI):

  1. etcd-synthetic-load target create --name PROD-2 --api-server https://...
  2. etcd-synthetic-load target configure --id <id>
  3. etcd-synthetic-load target generate  --id <id>
  4. etcd-synthetic-load load-plan --plan <id> --dry-run
  5. etcd-synthetic-load load-plan --plan <id> --i-understand-this-stresses-etcd
  6. etcd-synthetic-load report / test / cleanup
  7. etcd-synthetic-load target delete --id <id> --yes

See docs/WORKFLOW.md and diagrams/workflow.svg.

Or all via container with a data mount:

  docker run --rm -v "$PWD/data:/data" -e OC_SERVER -e OC_USER -e OC_PASSWORD \
    IMAGE generate --runtime /data/runtime.yaml --target /data/target.yaml

Cluster auth (checked in this order):
  1. --kubeconfig <path>
  2. $KUBECONFIG
  3. $OC_SERVER + $OC_USER + $OC_PASSWORD  (shells out to 'oc login')
  4. in-cluster config (when running as a Pod)
  5. ~/.kube/config

Legacy commands 'plan' / 'load' (Secrets+ConfigMaps profile.yaml) remain available.
`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&gf.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then OC_SERVER/OC_USER/OC_PASSWORD, then in-cluster, then ~/.kube/config)")

	root.AddCommand(newConfigureCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newTargetCmd())
	root.AddCommand(newLoadPlanCmd(gf))
	root.AddCommand(newReportCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newPlanCmd())   // legacy
	root.AddCommand(newLoadCmd(gf)) // legacy
	root.AddCommand(newStatusCmd(gf))
	root.AddCommand(newCleanupCmd(gf))

	return root
}
