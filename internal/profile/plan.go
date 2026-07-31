package profile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"time"
)

// PlanOptions are the CLI-facing knobs for `plan`. Zero values mean "use
// the built-in default", so callers can leave most fields unset.
type PlanOptions struct {
	TotalPayloadGiB    float64
	TotalSecrets       int
	TotalConfigMaps    int
	HelmSecretFraction float64
	NamespacePrefix    string

	SmallNamespaceCount  int
	MediumNamespaceCount int
	LargeNamespaceCount  int

	SmallSecretFraction  float64
	MediumSecretFraction float64
	LargeSecretFraction  float64

	SmallConfigMapFraction  float64
	MediumConfigMapFraction float64
	LargeConfigMapFraction  float64

	SmallBaseSecretPayloadBytes  int
	MediumBaseSecretPayloadBytes int
	LargeBaseSecretPayloadBytes  int

	SmallBaseConfigMapPayloadBytes  int
	MediumBaseConfigMapPayloadBytes int
	LargeBaseConfigMapPayloadBytes  int

	HelmSecretPayloadMultiplier float64

	PerObjectOverheadBytes int
}

// DefaultPlanOptions returns the built-in defaults, tuned so that the
// resulting profile lands close to the reference target the tool was
// designed around: ~5.6 GiB payload, ~120,000 Secrets, ~80,000 ConfigMaps,
// spread across a Small/Medium/Large namespace mix.
func DefaultPlanOptions() PlanOptions {
	return PlanOptions{
		TotalPayloadGiB:    5.6,
		TotalSecrets:       120000,
		TotalConfigMaps:    80000,
		HelmSecretFraction: 0.15,
		NamespacePrefix:    "esl",

		// Relative namespace population. Small tier has the most
		// namespaces but the fewest objects/namespace; Large tier has few
		// namespaces but is very dense - this mirrors typical real
		// clusters where a handful of "noisy" workloads (CI, GitOps,
		// service-mesh, logging) dominate etcd usage.
		SmallNamespaceCount:  60,
		MediumNamespaceCount: 30,
		LargeNamespaceCount:  10,

		// Object count distribution across tiers (should sum to ~1.0).
		SmallSecretFraction:  0.15,
		MediumSecretFraction: 0.35,
		LargeSecretFraction:  0.50,

		SmallConfigMapFraction:  0.15,
		MediumConfigMapFraction: 0.35,
		LargeConfigMapFraction:  0.50,

		// Relative (pre-scale) payload sizes in bytes - ratio matters more
		// than absolute value since `plan` rescales to hit the GiB target.
		SmallBaseSecretPayloadBytes:  100,
		MediumBaseSecretPayloadBytes: 1500,
		LargeBaseSecretPayloadBytes:  10000,

		SmallBaseConfigMapPayloadBytes:  100,
		MediumBaseConfigMapPayloadBytes: 1500,
		LargeBaseConfigMapPayloadBytes:  10000,

		// Helm release secrets embed a full rendered chart manifest and
		// tend to run several times larger than a generic app secret.
		HelmSecretPayloadMultiplier: 4.0,

		// ~350 bytes/object is a conservative placeholder for k8s object
		// metadata + etcd/boltdb/raft framing overhead. Tune with
		// --per-object-overhead-bytes once you've measured your own
		// cluster's etcd growth per object.
		PerObjectOverheadBytes: 350,
	}
}

// Generate builds a fully-computed Profile from the given options, scaling
// tier payload sizes uniformly (preserving their relative ratio) so that
// the estimated total payload approaches opts.TotalPayloadGiB while object
// counts hit the requested totals exactly (modulo integer rounding).
func Generate(opts PlanOptions) (*Profile, error) {
	if opts.TotalSecrets < 0 || opts.TotalConfigMaps < 0 {
		return nil, fmt.Errorf("target counts must be >= 0")
	}
	if opts.HelmSecretFraction < 0 || opts.HelmSecretFraction > 1 {
		return nil, fmt.Errorf("helm secret fraction must be in [0,1], got %v", opts.HelmSecretFraction)
	}

	tiers := []Tier{
		{
			Name:                        "small",
			NamespaceCount:              opts.SmallNamespaceCount,
			SecretFraction:              opts.SmallSecretFraction,
			ConfigMapFraction:           opts.SmallConfigMapFraction,
			BaseSecretPayloadBytes:      opts.SmallBaseSecretPayloadBytes,
			BaseConfigMapPayloadBytes:   opts.SmallBaseConfigMapPayloadBytes,
			HelmSecretPayloadMultiplier: opts.HelmSecretPayloadMultiplier,
		},
		{
			Name:                        "medium",
			NamespaceCount:              opts.MediumNamespaceCount,
			SecretFraction:              opts.MediumSecretFraction,
			ConfigMapFraction:           opts.MediumConfigMapFraction,
			BaseSecretPayloadBytes:      opts.MediumBaseSecretPayloadBytes,
			BaseConfigMapPayloadBytes:   opts.MediumBaseConfigMapPayloadBytes,
			HelmSecretPayloadMultiplier: opts.HelmSecretPayloadMultiplier,
		},
		{
			Name:                        "large",
			NamespaceCount:              opts.LargeNamespaceCount,
			SecretFraction:              opts.LargeSecretFraction,
			ConfigMapFraction:           opts.LargeConfigMapFraction,
			BaseSecretPayloadBytes:      opts.LargeBaseSecretPayloadBytes,
			BaseConfigMapPayloadBytes:   opts.LargeBaseConfigMapPayloadBytes,
			HelmSecretPayloadMultiplier: opts.HelmSecretPayloadMultiplier,
		},
	}

	normalizeFractions(tiers, func(t *Tier) *float64 { return &t.SecretFraction })
	normalizeFractions(tiers, func(t *Tier) *float64 { return &t.ConfigMapFraction })

	// Pass 1: derive raw per-tier object counts from fractions of the
	// targets, and per-namespace counts by dividing across NamespaceCount.
	for i := range tiers {
		t := &tiers[i]
		tierSecrets := int(math.Round(float64(opts.TotalSecrets) * t.SecretFraction))
		tierConfigMaps := int(math.Round(float64(opts.TotalConfigMaps) * t.ConfigMapFraction))
		t.Computed.TierTotalSecrets = tierSecrets
		t.Computed.TierTotalConfigMaps = tierConfigMaps
		if t.NamespaceCount > 0 {
			t.Computed.SecretsPerNamespace = ceilDiv(tierSecrets, t.NamespaceCount)
			t.Computed.ConfigMapsPerNamespace = ceilDiv(tierConfigMaps, t.NamespaceCount)
			t.Computed.HelmSecretsPerNamespace = int(math.Round(float64(t.Computed.SecretsPerNamespace) * opts.HelmSecretFraction))
			if t.Computed.HelmSecretsPerNamespace > t.Computed.SecretsPerNamespace {
				t.Computed.HelmSecretsPerNamespace = t.Computed.SecretsPerNamespace
			}
		}
	}

	// Recompute tier totals from the rounded per-namespace values so the
	// summary reflects exactly what `load` will create.
	for i := range tiers {
		t := &tiers[i]
		t.Computed.TierTotalSecrets = t.Computed.SecretsPerNamespace * t.NamespaceCount
		t.Computed.TierTotalConfigMaps = t.Computed.ConfigMapsPerNamespace * t.NamespaceCount
	}

	// Pass 2: figure out the uniform payload scale factor needed to hit
	// TotalPayloadGiB, given the *actual* rounded object counts and the
	// tiers' relative (base) payload sizes.
	var weightedBaseBytes float64
	var totalObjects int64
	for i := range tiers {
		t := &tiers[i]
		genericSecrets := t.Computed.SecretsPerNamespace - t.Computed.HelmSecretsPerNamespace
		if genericSecrets < 0 {
			genericSecrets = 0
		}
		helmSecrets := t.Computed.HelmSecretsPerNamespace
		perNsBase := float64(genericSecrets)*float64(t.BaseSecretPayloadBytes) +
			float64(helmSecrets)*float64(t.BaseSecretPayloadBytes)*t.HelmSecretPayloadMultiplier +
			float64(t.Computed.ConfigMapsPerNamespace)*float64(t.BaseConfigMapPayloadBytes)
		weightedBaseBytes += perNsBase * float64(t.NamespaceCount)
		totalObjects += int64(t.NamespaceCount) * int64(t.Computed.SecretsPerNamespace+t.Computed.ConfigMapsPerNamespace)
	}

	targetTotalBytes := opts.TotalPayloadGiB * 1024 * 1024 * 1024
	overheadBytes := float64(totalObjects) * float64(opts.PerObjectOverheadBytes)
	availableForPayload := targetTotalBytes - overheadBytes
	if availableForPayload < 0 {
		availableForPayload = 0
	}

	scale := 1.0
	if weightedBaseBytes > 0 {
		scale = availableForPayload / weightedBaseBytes
	}

	var estimatedPayloadBytes int64
	var totalHelmSecrets int
	for i := range tiers {
		t := &tiers[i]
		t.Computed.SecretPayloadBytes = maxInt(1, int(math.Round(float64(t.BaseSecretPayloadBytes)*scale)))
		t.Computed.HelmSecretPayloadBytes = maxInt(1, int(math.Round(float64(t.BaseSecretPayloadBytes)*t.HelmSecretPayloadMultiplier*scale)))
		t.Computed.ConfigMapPayloadBytes = maxInt(1, int(math.Round(float64(t.BaseConfigMapPayloadBytes)*scale)))

		genericSecrets := t.Computed.SecretsPerNamespace - t.Computed.HelmSecretsPerNamespace
		if genericSecrets < 0 {
			genericSecrets = 0
		}
		tierPayload := int64(t.NamespaceCount) * (int64(genericSecrets)*int64(t.Computed.SecretPayloadBytes) +
			int64(t.Computed.HelmSecretsPerNamespace)*int64(t.Computed.HelmSecretPayloadBytes) +
			int64(t.Computed.ConfigMapsPerNamespace)*int64(t.Computed.ConfigMapPayloadBytes))
		t.Computed.TierEstimatedPayloadByte = tierPayload
		estimatedPayloadBytes += tierPayload
		totalHelmSecrets += t.Computed.HelmSecretsPerNamespace * t.NamespaceCount
	}

	profileID, err := newProfileID()
	if err != nil {
		return nil, err
	}

	prefix := opts.NamespacePrefix
	if prefix == "" {
		prefix = "esl"
	}

	labels := map[string]string{
		LabelManaged:   "true",
		LabelProfileID: profileID,
	}

	var totalNamespaces, totalSecrets, totalConfigMaps int
	for _, t := range tiers {
		totalNamespaces += t.NamespaceCount
		totalSecrets += t.Computed.TierTotalSecrets
		totalConfigMaps += t.Computed.TierTotalConfigMaps
	}

	overheadTotal := int64(totalNamespaces)*0 + int64(totalSecrets+totalConfigMaps)*int64(opts.PerObjectOverheadBytes)
	estimatedTotalBytes := estimatedPayloadBytes + overheadTotal

	p := &Profile{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata: Metadata{
			ID:        profileID,
			CreatedAt: time.Now().UTC(),
			Generator: "plan",
		},
		Spec: Spec{
			Targets: Targets{
				TotalPayloadGiB: opts.TotalPayloadGiB,
				TotalSecrets:    opts.TotalSecrets,
				TotalConfigMaps: opts.TotalConfigMaps,
			},
			HelmSecretFraction:     opts.HelmSecretFraction,
			PerObjectOverheadBytes: opts.PerObjectOverheadBytes,
			NamespacePrefix:        prefix,
			Labels:                 labels,
			Tiers:                  tiers,
		},
		Summary: Summary{
			TotalNamespaces:        totalNamespaces,
			TotalSecrets:           totalSecrets,
			TotalHelmSecrets:       totalHelmSecrets,
			TotalConfigMaps:        totalConfigMaps,
			EstimatedPayloadBytes:  estimatedPayloadBytes,
			EstimatedPayloadGiB:    float64(estimatedPayloadBytes) / (1024 * 1024 * 1024),
			EstimatedOverheadBytes: overheadTotal,
			EstimatedTotalGiB:      float64(estimatedTotalBytes) / (1024 * 1024 * 1024),
			EtcdOverheadDisclaimer: "EstimatedTotalGiB is payload + a flat per-object overhead estimate " +
				"(perObjectOverheadBytes). Actual etcd DB size will differ due to object metadata, " +
				"managedFields, resourceVersion/compaction history, boltdb page overhead, and raft " +
				"log growth. Always validate against `oc get --raw /metrics | grep etcd_mvcc_db_total_size_in_bytes` " +
				"(or `etcdctl endpoint status`) on the target cluster rather than trusting this estimate alone.",
		},
	}

	return p, nil
}

func normalizeFractions(tiers []Tier, get func(*Tier) *float64) {
	var sum float64
	for i := range tiers {
		sum += *get(&tiers[i])
	}
	if sum <= 0 {
		// Even split fallback.
		for i := range tiers {
			*get(&tiers[i]) = 1.0 / float64(len(tiers))
		}
		return
	}
	if math.Abs(sum-1.0) < 1e-9 {
		return
	}
	for i := range tiers {
		f := get(&tiers[i])
		*f = *f / sum
	}
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return int(math.Ceil(float64(a) / float64(b)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newProfileID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("esl-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(b)), nil
}
