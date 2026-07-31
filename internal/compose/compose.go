package compose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/dasmlab/etcd-synthetic-load/internal/config"
)

// ObjectKind is a creatable synthetic object type.
type ObjectKind string

const (
	KindSecrets         ObjectKind = "secrets"
	KindConfigMaps      ObjectKind = "configmaps"
	KindServices        ObjectKind = "services"
	KindRoutes          ObjectKind = "routes"
	KindEgressFirewalls ObjectKind = "egressfirewalls"
	KindRoleBindings    ObjectKind = "rolebindings"
	KindServiceAccounts ObjectKind = "serviceaccounts"
)

// DefaultKinds matches etcd-size-per-namespace select-list (+ routes/egress).
var DefaultKinds = []ObjectKind{
	KindSecrets,
	KindConfigMaps,
	KindServices,
	KindRoutes,
	KindEgressFirewalls,
	KindRoleBindings,
	KindServiceAccounts,
}

// SizeRange is the SmallX..LargeX randomizer window for one kind in one tier.
type SizeRange struct {
	SmallX int `yaml:"smallX" json:"smallX"`
	LargeX int `yaml:"largeX" json:"largeX"`
}

// KindShare is how one object kind is allocated inside a t-shirt tier.
type KindShare struct {
	Kind             ObjectKind `yaml:"kind" json:"kind"`
	RecordCount      int        `yaml:"recordCount" json:"recordCount"`
	AvgBytes         int        `yaml:"avgBytes" json:"avgBytes"`
	TotalBytes       int64      `yaml:"totalBytes" json:"totalBytes"`
	SizeRange        SizeRange  `yaml:"sizeRange" json:"sizeRange"`
	PercentOfRecords float64    `yaml:"percentOfRecords" json:"percentOfRecords"`
	PercentOfSize    float64    `yaml:"percentOfSize" json:"percentOfSize"`
}

// TierPlan is the SMALL / MEDIUM / LARGE block.
type TierPlan struct {
	Name                   string      `yaml:"name" json:"name"`
	NamespaceCount         int         `yaml:"namespaceCount" json:"namespaceCount"`
	BytesPerNamespace      int64       `yaml:"bytesPerNamespace" json:"bytesPerNamespace"`
	TierBudgetBytes        int64       `yaml:"tierBudgetBytes" json:"tierBudgetBytes"`
	TotalRecords           int         `yaml:"totalRecords" json:"totalRecords"`
	TotalSizeBytes         int64       `yaml:"totalSizeBytes" json:"totalSizeBytes"`
	AvgBytesPerRecord      int         `yaml:"avgBytesPerRecord" json:"avgBytesPerRecord"`
	PercentOfRecordsTarget float64     `yaml:"percentOfRecordsTarget" json:"percentOfRecordsTarget"`
	PercentOfSizeTarget    float64     `yaml:"percentOfSizeTarget" json:"percentOfSizeTarget"`
	Composition            []KindShare `yaml:"composition" json:"composition"`
}

// Plan is the generated load map written to disk / shown in UI.
type Plan struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   PlanMeta    `yaml:"metadata" json:"metadata"`
	Target     PlanTarget  `yaml:"target" json:"target"`
	Tiers      []TierPlan  `yaml:"tiers" json:"tiers"`
	Summary    PlanSummary `yaml:"summary" json:"summary"`
}

type PlanMeta struct {
	ID        string    `yaml:"id" json:"id"`
	Name      string    `yaml:"name" json:"name"`
	CreatedAt time.Time `yaml:"createdAt" json:"createdAt"`
	Cluster   string    `yaml:"cluster" json:"cluster"`
	Notes     string    `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type PlanTarget struct {
	UtilizationGiB  float64        `yaml:"utilizationGiB" json:"utilizationGiB"`
	UtilizationMode string         `yaml:"utilizationMode" json:"utilizationMode"`
	AssumedQuotaGiB float64        `yaml:"assumedQuotaGiB,omitempty" json:"assumedQuotaGiB,omitempty"`
	ObjectTotals    map[string]int `yaml:"objectTotals" json:"objectTotals"`
}

type PlanSummary struct {
	TotalNamespaces int     `yaml:"totalNamespaces" json:"totalNamespaces"`
	TotalRecords    int     `yaml:"totalRecords" json:"totalRecords"`
	TotalSizeBytes  int64   `yaml:"totalSizeBytes" json:"totalSizeBytes"`
	TotalSizeGiB    float64 `yaml:"totalSizeGiB" json:"totalSizeGiB"`
	Disclaimer      string  `yaml:"disclaimer" json:"disclaimer"`
}

// GenerateInput is the dynamic request (from YAML, CLI, or API).
type GenerateInput struct {
	Name               string
	ClusterDisplayName string
	UtilizationGiB     *float64
	UtilizationPercent *float64
	AssumedQuotaGiB    float64
	Objects            map[ObjectKind]int // legacy global totals (optional)
	// Seed is the preferred Generation Seed (namespace budgets + composition).
	// When nil, DefaultSeed(utilization) is used.
	Seed *SeedConfig
}

func ResolveUtilization(in GenerateInput) (gib float64, mode string, quota float64, err error) {
	quota = in.AssumedQuotaGiB
	if quota <= 0 {
		quota = 8.0
	}
	if in.UtilizationGiB != nil && in.UtilizationPercent != nil {
		return 0, "", 0, fmt.Errorf("specify only one of utilizationGiB or utilizationPercent")
	}
	if in.UtilizationPercent != nil {
		p := *in.UtilizationPercent
		if p <= 0 || p > 100 {
			return 0, "", 0, fmt.Errorf("utilizationPercent must be in (0,100]")
		}
		return quota * (p / 100.0), "percent", quota, nil
	}
	if in.UtilizationGiB != nil {
		g := *in.UtilizationGiB
		if g <= 0 {
			return 0, "", 0, fmt.Errorf("utilizationGiB must be > 0")
		}
		return g, "gib", quota, nil
	}
	return 5.0, "gib-default", quota, nil
}

// Generate builds a Plan from a Generation Seed (namespace budgets + composition).
// Impossible seeds (composition > per-ns budget, or tier sum far from target) error out.
func Generate(in GenerateInput) (*Plan, error) {
	gib, mode, quota, err := ResolveUtilization(in)
	if err != nil {
		return nil, err
	}

	seed := DefaultSeed(gib)
	if in.Seed != nil {
		seed = *in.Seed
		if seed.UtilizationGiB <= 0 {
			seed.UtilizationGiB = gib
		}
	} else if in.AssumedQuotaGiB > 0 {
		seed.AssumedQuotaGiB = in.AssumedQuotaGiB
	}
	if quota > 0 {
		seed.AssumedQuotaGiB = quota
	}
	seed.UtilizationGiB = gib

	// Never allow impossible N×size/ns or composition overflow through to a plan.
	seed, _ = ClampToFeasible(seed)

	preview := Preview(seed)
	if !preview.OK {
		msgs := make([]string, 0, len(preview.Issues))
		for _, iss := range preview.Issues {
			if iss.Level == "error" {
				msgs = append(msgs, iss.Message)
			}
		}
		if len(msgs) == 0 {
			msgs = append(msgs, "seed failed validation")
		}
		return nil, fmt.Errorf("impossible seed config: %s", strings.Join(msgs, "; "))
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	targetBytes := int64(gib * float64(GiB))

	plan := &Plan{
		APIVersion: "etcd-synthetic-load.dasmlab.org/v1",
		Kind:       "LoadPlan",
		Metadata: PlanMeta{
			ID:        id,
			Name:      coalesce(in.Name, "generated"),
			CreatedAt: time.Now().UTC(),
			Cluster:   coalesce(in.ClusterDisplayName, "unknown"),
			Notes:     "Budget-first plan: N namespaces × size/ns must sum to utilization; composition must fit per-ns budget.",
		},
		Target: PlanTarget{
			UtilizationGiB:  seed.UtilizationGiB,
			UtilizationMode: mode,
			AssumedQuotaGiB: seed.AssumedQuotaGiB,
			ObjectTotals:    map[string]int{},
		},
	}

	var sumRecords int
	var sumBytes int64
	var sumNS int

	for _, t := range seed.Tiers {
		tp := TierPlan{
			Name:              t.Name,
			NamespaceCount:    t.NamespaceCount,
			BytesPerNamespace: t.BytesPerNamespace,
			TierBudgetBytes:   t.TierBudgetBytes(),
		}
		var tierRecords int
		var tierBytes int64
		for _, k := range t.Composition {
			if !k.Enabled || k.RecordsPerNamespace <= 0 {
				continue
			}
			totalRec := k.RecordsPerNamespace * t.NamespaceCount
			avg := k.AvgBytes()
			total := int64(totalRec) * int64(avg)
			ks := KindShare{
				Kind:        k.Kind,
				RecordCount: totalRec,
				AvgBytes:    avg,
				TotalBytes:  total,
				SizeRange:   SizeRange{SmallX: k.SmallX, LargeX: k.LargeX},
			}
			tp.Composition = append(tp.Composition, ks)
			tierRecords += totalRec
			tierBytes += total
			plan.Target.ObjectTotals[string(k.Kind)] += totalRec
		}
		for j := range tp.Composition {
			if tierRecords > 0 {
				tp.Composition[j].PercentOfRecords = 100 * float64(tp.Composition[j].RecordCount) / float64(tierRecords)
			}
			if tierBytes > 0 {
				tp.Composition[j].PercentOfSize = 100 * float64(tp.Composition[j].TotalBytes) / float64(tierBytes)
			}
		}
		tp.TotalRecords = tierRecords
		tp.TotalSizeBytes = tierBytes
		if tierRecords > 0 {
			tp.AvgBytesPerRecord = int(tierBytes / int64(tierRecords))
		}
		if targetBytes > 0 {
			tp.PercentOfSizeTarget = 100 * float64(tp.TierBudgetBytes) / float64(targetBytes)
		}
		plan.Tiers = append(plan.Tiers, tp)
		sumRecords += tierRecords
		sumBytes += tierBytes
		sumNS += t.NamespaceCount
	}

	for i := range plan.Tiers {
		if sumRecords > 0 {
			plan.Tiers[i].PercentOfRecordsTarget = 100 * float64(plan.Tiers[i].TotalRecords) / float64(sumRecords)
		}
	}

	plan.Summary = PlanSummary{
		TotalNamespaces: sumNS,
		TotalRecords:    sumRecords,
		TotalSizeBytes:  sumBytes,
		TotalSizeGiB:    float64(sumBytes) / float64(GiB),
		Disclaimer:      "Totals are estimates from avg(SmallX,LargeX) within per-namespace budgets. Real etcd DB size includes overhead, history, and fragmentation.",
	}
	return plan, nil
}

// FormatReport prints the human-readable SMALL/MEDIUM/LARGE composition.
func FormatReport(p *Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT\n")
	fmt.Fprintf(&b, "Load plan %s  cluster=%s  target=%.2f GiB (%s)\n", p.Metadata.ID, p.Metadata.Cluster, p.Target.UtilizationGiB, p.Target.UtilizationMode)
	fmt.Fprintf(&b, "Estimated total: %.3f GiB across %d records in %d namespaces\n\n", p.Summary.TotalSizeGiB, p.Summary.TotalRecords, p.Summary.TotalNamespaces)

	for _, t := range p.Tiers {
		fmt.Fprintf(&b, "%s\n", t.Name)
		fmt.Fprintf(&b, "  - Namespaces:                         %d × %s/ns = %s tier budget\n",
			t.NamespaceCount, human(t.BytesPerNamespace), human(t.TierBudgetBytes))
		fmt.Fprintf(&b, "  - Total Size per Record (avg):         %s\n", human(int64(t.AvgBytesPerRecord)))
		fmt.Fprintf(&b, "  - Total Size of All Records:           %s\n", human(t.TotalSizeBytes))
		fmt.Fprintf(&b, "  - Total Number of Records:             %d\n", t.TotalRecords)
		fmt.Fprintf(&b, "  - %% of Records vs Utilization Target:  %.1f%%\n", t.PercentOfRecordsTarget)
		fmt.Fprintf(&b, "  - %% of Size (tier budget) vs Target:   %.1f%%\n", t.PercentOfSizeTarget)
		fmt.Fprintf(&b, "  - Composition (per namespace must fit %s)\n", human(t.BytesPerNamespace))
		for _, c := range t.Composition {
			perNS := 0
			if t.NamespaceCount > 0 {
				perNS = c.RecordCount / t.NamespaceCount
			}
			fmt.Fprintf(&b, "       - %d %s (%d/ns)  (SmallX: %s, LargeX: %s)  avg=%s  total=%s  (%.1f%% records / %.1f%% size)\n",
				c.RecordCount, c.Kind, perNS,
				human(int64(c.SizeRange.SmallX)), human(int64(c.SizeRange.LargeX)),
				human(int64(c.AvgBytes)), human(c.TotalBytes),
				c.PercentOfRecords, c.PercentOfSize,
			)
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "%s\n", p.Summary.Disclaimer)
	return b.String()
}

func FromLoadTarget(t *config.LoadTarget, clusterName string) GenerateInput {
	in := GenerateInput{
		Name:               t.Metadata.Name,
		ClusterDisplayName: clusterName,
		Objects:            map[ObjectKind]int{},
	}
	if t.Utilization.TargetGiB != nil {
		in.UtilizationGiB = t.Utilization.TargetGiB
	}
	if t.Utilization.TargetPercentOfQuota != nil {
		in.UtilizationPercent = t.Utilization.TargetPercentOfQuota
	}
	if t.Utilization.AssumedQuotaGiB != nil {
		in.AssumedQuotaGiB = *t.Utilization.AssumedQuotaGiB
	}
	// Prefer explicit seed tiers from YAML when present.
	if len(t.SeedTiers) > 0 {
		gib := 5.0
		if in.UtilizationGiB != nil {
			gib = *in.UtilizationGiB
		}
		seed := SeedConfig{UtilizationGiB: gib, AssumedQuotaGiB: in.AssumedQuotaGiB}
		for _, st := range t.SeedTiers {
			seed.Tiers = append(seed.Tiers, TierSeed{
				Name:              st.Name,
				NamespaceCount:    st.NamespaceCount,
				BytesPerNamespace: st.BytesPerNamespace,
				Composition:       toKindSpecs(st.Composition),
			})
		}
		in.Seed = &seed
	}
	return in
}

func toKindSpecs(in []config.KindSpecYAML) []KindSpec {
	out := make([]KindSpec, 0, len(in))
	for _, k := range in {
		out = append(out, KindSpec{
			Kind:                ObjectKind(k.Kind),
			Enabled:             k.Enabled,
			RecordsPerNamespace: k.RecordsPerNamespace,
			SmallX:              k.SmallX,
			LargeX:              k.LargeX,
		})
	}
	return out
}

func human(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.2f GiB", float64(n)/(1024*1024*1024))
	}
	if n >= 1024*1024 {
		return fmt.Sprintf("%.2f MiB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.2f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:]), nil
}
