package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

func newPlanCmd() *cobra.Command {
	opts := profile.DefaultPlanOptions()
	var output string

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Generate a Small/Medium/Large namespace profile that sums toward the given targets",
		Long: `plan computes a tunable Small/Medium/Large namespace profile (namespace
counts, objects-per-namespace, and payload sizes) that sums toward the
requested aggregate targets, and writes it to profile.yaml.

Object counts (Secrets, ConfigMaps) hit the requested totals exactly
(modulo integer rounding). Payload sizes are scaled uniformly (preserving
the relative Small < Medium < Large ratio) so the *estimated* payload
volume approaches --target-gib. See the emitted profile.yaml
"etcdOverheadDisclaimer" field for why estimated payload != real etcd DB
size.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := profile.Generate(opts)
			if err != nil {
				return err
			}
			if err := profile.Save(p, output); err != nil {
				return err
			}

			fmt.Printf("Wrote %s (profile id: %s)\n\n", output, p.Metadata.ID)
			fmt.Println("Summary:")
			fmt.Printf("  Namespaces:        %d\n", p.Summary.TotalNamespaces)
			fmt.Printf("  Secrets:           %d  (of which helm-release style: %d)\n", p.Summary.TotalSecrets, p.Summary.TotalHelmSecrets)
			fmt.Printf("  ConfigMaps:        %d\n", p.Summary.TotalConfigMaps)
			fmt.Printf("  Est. payload:      %.2f GiB\n", p.Summary.EstimatedPayloadGiB)
			fmt.Printf("  Est. incl overhead:%.2f GiB\n", p.Summary.EstimatedTotalGiB)
			fmt.Println()
			for _, t := range p.Spec.Tiers {
				fmt.Printf("  [%-6s] namespaces=%-4d secrets/ns=%-5d (helm=%-4d) cm/ns=%-5d secretPayload=%-6dB cmPayload=%-6dB helmPayload=%-7dB\n",
					t.Name, t.NamespaceCount, t.Computed.SecretsPerNamespace, t.Computed.HelmSecretsPerNamespace,
					t.Computed.ConfigMapsPerNamespace, t.Computed.SecretPayloadBytes, t.Computed.ConfigMapPayloadBytes,
					t.Computed.HelmSecretPayloadBytes)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.Float64Var(&opts.TotalPayloadGiB, "target-gib", opts.TotalPayloadGiB, "target estimated payload size, in GiB")
	f.IntVar(&opts.TotalSecrets, "target-secrets", opts.TotalSecrets, "target total number of Secrets")
	f.IntVar(&opts.TotalConfigMaps, "target-configmaps", opts.TotalConfigMaps, "target total number of ConfigMaps")
	f.Float64Var(&opts.HelmSecretFraction, "helm-secret-fraction", opts.HelmSecretFraction, "fraction (0-1) of secrets generated as helm-release-style secrets")
	f.StringVar(&opts.NamespacePrefix, "namespace-prefix", opts.NamespacePrefix, "prefix for generated namespace names")
	f.IntVar(&opts.PerObjectOverheadBytes, "per-object-overhead-bytes", opts.PerObjectOverheadBytes, "estimated non-payload bytes/object (metadata+etcd framing) used only for the GiB estimate")

	f.IntVar(&opts.SmallNamespaceCount, "small-namespaces", opts.SmallNamespaceCount, "number of Small-tier namespaces")
	f.IntVar(&opts.MediumNamespaceCount, "medium-namespaces", opts.MediumNamespaceCount, "number of Medium-tier namespaces")
	f.IntVar(&opts.LargeNamespaceCount, "large-namespaces", opts.LargeNamespaceCount, "number of Large-tier namespaces")

	f.Float64Var(&opts.SmallSecretFraction, "small-secret-fraction", opts.SmallSecretFraction, "fraction of target-secrets allocated to the Small tier")
	f.Float64Var(&opts.MediumSecretFraction, "medium-secret-fraction", opts.MediumSecretFraction, "fraction of target-secrets allocated to the Medium tier")
	f.Float64Var(&opts.LargeSecretFraction, "large-secret-fraction", opts.LargeSecretFraction, "fraction of target-secrets allocated to the Large tier")

	f.Float64Var(&opts.SmallConfigMapFraction, "small-configmap-fraction", opts.SmallConfigMapFraction, "fraction of target-configmaps allocated to the Small tier")
	f.Float64Var(&opts.MediumConfigMapFraction, "medium-configmap-fraction", opts.MediumConfigMapFraction, "fraction of target-configmaps allocated to the Medium tier")
	f.Float64Var(&opts.LargeConfigMapFraction, "large-configmap-fraction", opts.LargeConfigMapFraction, "fraction of target-configmaps allocated to the Large tier")

	f.IntVar(&opts.SmallBaseSecretPayloadBytes, "small-secret-payload-bytes", opts.SmallBaseSecretPayloadBytes, "relative (pre-scale) Small tier secret payload size in bytes")
	f.IntVar(&opts.MediumBaseSecretPayloadBytes, "medium-secret-payload-bytes", opts.MediumBaseSecretPayloadBytes, "relative (pre-scale) Medium tier secret payload size in bytes")
	f.IntVar(&opts.LargeBaseSecretPayloadBytes, "large-secret-payload-bytes", opts.LargeBaseSecretPayloadBytes, "relative (pre-scale) Large tier secret payload size in bytes")

	f.IntVar(&opts.SmallBaseConfigMapPayloadBytes, "small-configmap-payload-bytes", opts.SmallBaseConfigMapPayloadBytes, "relative (pre-scale) Small tier configmap payload size in bytes")
	f.IntVar(&opts.MediumBaseConfigMapPayloadBytes, "medium-configmap-payload-bytes", opts.MediumBaseConfigMapPayloadBytes, "relative (pre-scale) Medium tier configmap payload size in bytes")
	f.IntVar(&opts.LargeBaseConfigMapPayloadBytes, "large-configmap-payload-bytes", opts.LargeBaseConfigMapPayloadBytes, "relative (pre-scale) Large tier configmap payload size in bytes")

	f.Float64Var(&opts.HelmSecretPayloadMultiplier, "helm-secret-payload-multiplier", opts.HelmSecretPayloadMultiplier, "helm-release secret payload = tier base secret payload * this multiplier")

	f.StringVarP(&output, "output", "o", "profile.yaml", "output path for the generated profile")

	return cmd
}
