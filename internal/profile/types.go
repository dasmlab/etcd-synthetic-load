// Package profile defines the on-disk profile.yaml schema used to describe
// a synthetic etcd load shape (Small/Medium/Large namespace tiers) and the
// math used to derive that shape from high level targets.
package profile

import "time"

const (
	// LabelManaged marks every namespace/object created by this tool so
	// that `status` and `cleanup` can safely find (and only find) objects
	// this tool owns.
	LabelManaged = "etcd-synthetic-load.dasmlab.org/managed"
	// LabelProfileID records which profile (run) an object belongs to.
	LabelProfileID = "etcd-synthetic-load.dasmlab.org/profile-id"
	// LabelTier records which S/M/L tier a namespace/object belongs to.
	LabelTier = "etcd-synthetic-load.dasmlab.org/tier"
	// LabelKind marks the synthetic "flavor" of an object, e.g. "generic"
	// or "helm-release", so triage tooling can distinguish impact classes.
	LabelKind = "etcd-synthetic-load.dasmlab.org/kind"

	APIVersion = "etcd-synthetic-load.dasmlab.org/v1"
	Kind       = "Profile"

	// KindGeneric / KindHelmRelease are the values used for LabelKind.
	KindGeneric    = "generic"
	KindHelmSecret = "helm-release"
)

// Profile is the full on-disk representation written by `plan` and consumed
// by `load`, `status`, and `cleanup`.
type Profile struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
	Summary    Summary  `yaml:"summary"`
}

type Metadata struct {
	ID        string    `yaml:"id"`
	CreatedAt time.Time `yaml:"createdAt"`
	Generator string    `yaml:"generator"`
	Notes     string    `yaml:"notes,omitempty"`
}

// Targets are the high level goals passed to `plan`.
type Targets struct {
	// TotalPayloadGiB is the *estimated* aggregate size of object payload
	// data (Secret/ConfigMap `data` fields), in GiB. This is NOT the same
	// as the resulting etcd DB size: etcd/Kubernetes add per-object
	// overhead (metadata, resourceVersion history, boltdb page overhead,
	// protobuf framing, etc). See Summary.EstimatedEtcdOverheadNote.
	TotalPayloadGiB float64 `yaml:"totalPayloadGiB"`
	TotalSecrets    int     `yaml:"totalSecrets"`
	TotalConfigMaps int     `yaml:"totalConfigMaps"`
}

// Spec is the tunable shape of the load: how Targets are distributed across
// S/M/L tiers, and what fraction of secrets mimic helm-release secrets.
type Spec struct {
	Targets Targets `yaml:"targets"`

	// HelmSecretFraction is the fraction (0.0-1.0) of TotalSecrets that are
	// generated as helm-release-style secrets (type helm.sh/release.v1)
	// instead of generic Opaque secrets. Helm release secrets are a
	// meaningful real-world etcd impact category because they are large,
	// numerous (one per release per revision), and frequently forgotten by
	// `helm uninstall --keep-history` or failed migrations.
	HelmSecretFraction float64 `yaml:"helmSecretFraction"`

	// PerObjectOverheadBytes is a fixed estimate of the non-payload bytes
	// each object contributes to etcd (object metadata: name, namespace,
	// labels, annotations, managedFields, resourceVersion, uid, etc, plus
	// etcd/boltdb/raft framing). Used only to produce a more realistic
	// EstimatedPayloadGiB in the Summary; actual etcd usage will vary by
	// cluster/version and should always be measured, not assumed.
	PerObjectOverheadBytes int `yaml:"perObjectOverheadBytes"`

	NamespacePrefix string            `yaml:"namespacePrefix"`
	Labels          map[string]string `yaml:"labels"`

	Tiers []Tier `yaml:"tiers"`
}

// Tier describes one Small/Medium/Large style namespace population.
type Tier struct {
	// Name is a free-form label, conventionally "small", "medium", "large".
	Name string `yaml:"name"`

	// NamespaceCount is how many namespaces this tier creates.
	NamespaceCount int `yaml:"namespaceCount"`

	// SecretFraction / ConfigMapFraction: fraction (0.0-1.0) of
	// Targets.TotalSecrets / TotalConfigMaps allocated to this tier. The
	// fractions across all tiers should sum to ~1.0 (plan will normalize
	// if they don't).
	SecretFraction    float64 `yaml:"secretFraction"`
	ConfigMapFraction float64 `yaml:"configMapFraction"`

	// BaseSecretPayloadBytes / BaseConfigMapPayloadBytes are the
	// *relative* payload sizes (in bytes, of the base64-ish `data` value)
	// before `plan` scales all tiers uniformly to hit
	// Targets.TotalPayloadGiB. Keep the ratio between tiers meaningful
	// (e.g. small=100, medium=2000, large=10000) - `plan` preserves the
	// ratio while scaling the absolute magnitude.
	BaseSecretPayloadBytes    int `yaml:"baseSecretPayloadBytes"`
	BaseConfigMapPayloadBytes int `yaml:"baseConfigMapPayloadBytes"`

	// HelmSecretPayloadMultiplier: helm-release-style secrets in this tier
	// get BaseSecretPayloadBytes * HelmSecretPayloadMultiplier as their
	// base payload (helm release manifests are typically much larger than
	// a generic app secret because they embed the full rendered chart).
	HelmSecretPayloadMultiplier float64 `yaml:"helmSecretPayloadMultiplier"`

	// Computed fields are filled in by `plan` and consumed directly by
	// `load`/`status` so those commands never need to re-derive math from
	// the raw targets/fractions above.
	Computed ComputedTier `yaml:"computed"`
}

// ComputedTier holds the fully resolved, ready-to-apply numbers for a tier.
type ComputedTier struct {
	SecretsPerNamespace      int   `yaml:"secretsPerNamespace"`
	HelmSecretsPerNamespace  int   `yaml:"helmSecretsPerNamespace"` // subset of SecretsPerNamespace
	ConfigMapsPerNamespace   int   `yaml:"configMapsPerNamespace"`
	SecretPayloadBytes       int   `yaml:"secretPayloadBytes"`     // generic secret payload, post-scale
	HelmSecretPayloadBytes   int   `yaml:"helmSecretPayloadBytes"` // helm-release secret payload, post-scale
	ConfigMapPayloadBytes    int   `yaml:"configMapPayloadBytes"`  // post-scale
	TierTotalSecrets         int   `yaml:"tierTotalSecrets"`
	TierTotalConfigMaps      int   `yaml:"tierTotalConfigMaps"`
	TierEstimatedPayloadByte int64 `yaml:"tierEstimatedPayloadBytes"`
}

// Summary is a human-friendly rollup, recomputed and rewritten every time
// `plan` runs, so `profile.yaml` is self-describing without re-running math.
type Summary struct {
	TotalNamespaces        int     `yaml:"totalNamespaces"`
	TotalSecrets           int     `yaml:"totalSecrets"`
	TotalHelmSecrets       int     `yaml:"totalHelmSecrets"`
	TotalConfigMaps        int     `yaml:"totalConfigMaps"`
	EstimatedPayloadBytes  int64   `yaml:"estimatedPayloadBytes"`
	EstimatedPayloadGiB    float64 `yaml:"estimatedPayloadGiB"`
	EstimatedOverheadBytes int64   `yaml:"estimatedOverheadBytes"`
	EstimatedTotalGiB      float64 `yaml:"estimatedTotalGiB"`
	EtcdOverheadDisclaimer string  `yaml:"etcdOverheadDisclaimer"`
}
