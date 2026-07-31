package compose

import (
	"fmt"
	"math"
)

// MiB / GiB helpers for seed budgets (powers of 1024).
const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB
)

const (
	KindEndpoints      ObjectKind = "endpoints"
	KindEndpointSlices ObjectKind = "endpointslices"
	KindResourceQuotas ObjectKind = "resourcequotas"
	KindLimitRanges    ObjectKind = "limitranges"
)

// PickListKinds is the etcd-size-per-namespace default set (+ routes).
// UI presents these as a multi-select; only enabled kinds enter composition.
var PickListKinds = []ObjectKind{
	KindSecrets,
	KindConfigMaps,
	KindServices,
	KindEndpoints,
	KindEndpointSlices,
	KindRoleBindings,
	KindServiceAccounts,
	KindResourceQuotas,
	KindLimitRanges,
	KindEgressFirewalls,
	KindRoutes,
}

// KindSpec is per-namespace composition for one object kind inside a tier.
type KindSpec struct {
	Kind                ObjectKind `yaml:"kind" json:"kind"`
	Enabled             bool       `yaml:"enabled" json:"enabled"`
	RecordsPerNamespace int        `yaml:"recordsPerNamespace" json:"recordsPerNamespace"`
	SmallX              int        `yaml:"smallX" json:"smallX"` // bytes
	LargeX              int        `yaml:"largeX" json:"largeX"` // bytes
}

// AvgBytes is the mid-point of SmallX..LargeX (what we size against for budgets).
func (k KindSpec) AvgBytes() int {
	lo, hi := k.SmallX, k.LargeX
	if lo <= 0 {
		lo = 1
	}
	if hi < lo {
		hi = lo
	}
	return (lo + hi) / 2
}

// BytesPerNamespace returns estimated payload for this kind in one namespace.
func (k KindSpec) BytesPerNamespace() int64 {
	if !k.Enabled || k.RecordsPerNamespace <= 0 {
		return 0
	}
	return int64(k.RecordsPerNamespace) * int64(k.AvgBytes())
}

// TierSeed is the editable SMALL/MEDIUM/LARGE block:
//
//	namespaceCount × bytesPerNamespace = tier budget
//
// Composition must fit inside bytesPerNamespace.
type TierSeed struct {
	Name              string     `yaml:"name" json:"name"`
	NamespaceCount    int        `yaml:"namespaceCount" json:"namespaceCount"`
	BytesPerNamespace int64      `yaml:"bytesPerNamespace" json:"bytesPerNamespace"`
	Composition       []KindSpec `yaml:"composition" json:"composition"`
}

// TierBudgetBytes is N × size-per-ns.
func (t TierSeed) TierBudgetBytes() int64 {
	if t.NamespaceCount <= 0 || t.BytesPerNamespace <= 0 {
		return 0
	}
	return int64(t.NamespaceCount) * t.BytesPerNamespace
}

// CompositionBytesPerNamespace sums enabled kinds' per-ns payload.
func (t TierSeed) CompositionBytesPerNamespace() int64 {
	var sum int64
	for _, k := range t.Composition {
		sum += k.BytesPerNamespace()
	}
	return sum
}

// SeedConfig is the Generation Seed — utilization + feasible S/M/L budgets.
type SeedConfig struct {
	UtilizationGiB  float64    `yaml:"utilizationGiB" json:"utilizationGiB"`
	AssumedQuotaGiB float64    `yaml:"assumedQuotaGiB,omitempty" json:"assumedQuotaGiB,omitempty"`
	Tiers           []TierSeed `yaml:"tiers" json:"tiers"`
}

// ValidationIssue describes an impossible or inconsistent seed setting.
type ValidationIssue struct {
	Level   string `json:"level"` // "error" | "warning"
	Tier    string `json:"tier,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SeedPreview is the reactive response for the UI (budgets + feasibility).
type SeedPreview struct {
	OK                    bool              `json:"ok"`
	UtilizationBytes      int64             `json:"utilizationBytes"`
	UtilizationGiB        float64           `json:"utilizationGiB"`
	TierBudgetsTotalBytes int64             `json:"tierBudgetsTotalBytes"`
	TierBudgetsTotalGiB   float64           `json:"tierBudgetsTotalGiB"`
	BudgetDeltaBytes      int64             `json:"budgetDeltaBytes"` // tiers − target (0 = exact)
	BudgetDeltaPct        float64           `json:"budgetDeltaPct"`
	Tiers                 []TierPreview     `json:"tiers"`
	Issues                []ValidationIssue `json:"issues"`
}

// TierPreview is live budget math for one t-shirt card.
type TierPreview struct {
	Name                         string  `json:"name"`
	NamespaceCount               int     `json:"namespaceCount"`
	BytesPerNamespace            int64   `json:"bytesPerNamespace"`
	TierBudgetBytes              int64   `json:"tierBudgetBytes"`
	CompositionBytesPerNamespace int64   `json:"compositionBytesPerNamespace"`
	CompositionHeadroomBytes     int64   `json:"compositionHeadroomBytes"`
	CompositionUsedPct           float64 `json:"compositionUsedPct"`
	Fits                         bool    `json:"fits"`
	PercentOfUtilization         float64 `json:"percentOfUtilization"`
	// Hard caps so the UI cannot set N×size/ns past the utilization target.
	MaxBytesPerNamespace int64 `json:"maxBytesPerNamespace"`
	MaxNamespaceCount    int   `json:"maxNamespaceCount"`
}

// DefaultSeed returns a feasible starter matching the mental model:
//
//	10 SMALL × 100 MiB ≈ 1 GiB
//	40 MED   ×  50 MiB ≈ 2 GiB
//	 2 LARGE ×   1 GiB =  2 GiB
//	Total ≈ 5.0 GiB
//
// Note: "40 × 250 MiB" would be ~9.8 GiB by itself — that cannot fit a 5 GiB
// target. The MED default uses 50 MiB/ns so 40×50 MiB ≈ 2 GiB as intended.
func DefaultSeed(utilizationGiB float64) SeedConfig {
	if utilizationGiB <= 0 {
		utilizationGiB = 5.0
	}
	scale := utilizationGiB / 5.0

	smallPer := int64(float64(100*MiB) * scale)
	medPer := int64(float64(50*MiB) * scale)
	largePer := int64(float64(1*GiB) * scale)

	return SeedConfig{
		UtilizationGiB:  utilizationGiB,
		AssumedQuotaGiB: 8.0,
		Tiers: []TierSeed{
			{
				Name: "SMALL", NamespaceCount: 10, BytesPerNamespace: smallPer,
				Composition: defaultCompositionForBudget(smallPer, map[ObjectKind]float64{
					KindSecrets: 0.45, KindConfigMaps: 0.35, KindServices: 0.05,
					KindRoleBindings: 0.08, KindServiceAccounts: 0.05, KindEgressFirewalls: 0.02,
				}, SizeRange{SmallX: 2 * KiB, LargeX: 8 * KiB}),
			},
			{
				Name: "MEDIUM", NamespaceCount: 40, BytesPerNamespace: medPer,
				Composition: defaultCompositionForBudget(medPer, map[ObjectKind]float64{
					KindSecrets: 0.50, KindConfigMaps: 0.35, KindServices: 0.04,
					KindRoleBindings: 0.06, KindServiceAccounts: 0.04, KindEgressFirewalls: 0.01,
				}, SizeRange{SmallX: 8 * KiB, LargeX: 32 * KiB}),
			},
			{
				Name: "LARGE", NamespaceCount: 2, BytesPerNamespace: largePer,
				Composition: defaultCompositionForBudget(largePer, map[ObjectKind]float64{
					KindSecrets: 0.50, KindConfigMaps: 0.40, KindServices: 0.02,
					KindRoleBindings: 0.04, KindServiceAccounts: 0.03, KindEgressFirewalls: 0.01,
				}, SizeRange{SmallX: 32 * KiB, LargeX: 128 * KiB}),
			},
		},
	}
}

func defaultCompositionForBudget(budget int64, shares map[ObjectKind]float64, rng SizeRange) []KindSpec {
	avg := (rng.SmallX + rng.LargeX) / 2
	if avg <= 0 {
		avg = 1
	}
	out := make([]KindSpec, 0, len(PickListKinds))
	for _, kind := range PickListKinds {
		share, ok := shares[kind]
		if !ok || share <= 0 {
			out = append(out, KindSpec{Kind: kind, Enabled: false, SmallX: rng.SmallX, LargeX: rng.LargeX})
			continue
		}
		bytesForKind := int64(float64(budget) * share)
		n := int(bytesForKind / int64(avg))
		if n < 1 {
			n = 1
		}
		for int64(n)*int64(avg) > bytesForKind+int64(avg) && n > 1 {
			n--
		}
		out = append(out, KindSpec{
			Kind:                kind,
			Enabled:             true,
			RecordsPerNamespace: n,
			SmallX:              rng.SmallX,
			LargeX:              rng.LargeX,
		})
	}
	tmp := TierSeed{BytesPerNamespace: budget, Composition: out}
	for tmp.CompositionBytesPerNamespace() > budget {
		shrunk := false
		for i := range out {
			if out[i].Enabled && out[i].RecordsPerNamespace > 0 {
				out[i].RecordsPerNamespace--
				if out[i].RecordsPerNamespace == 0 {
					out[i].Enabled = false
				}
				shrunk = true
				break
			}
		}
		if !shrunk {
			break
		}
		tmp.Composition = out
	}
	return out
}

// Preview validates a seed with the default hard overshoot rule (>1 MiB over = error).
func Preview(seed SeedConfig) SeedPreview {
	return PreviewWithTolerance(seed, 0)
}

// PreviewWithTolerance validates a seed. tolerancePercent (e.g. 10) allows
// |tierBudgets − utilization| within that band. When tolerance is 0, overshoot
// by more than 1 MiB is always an error (strict mode used by Clamp checks).
func PreviewWithTolerance(seed SeedConfig, tolerancePercent float64) SeedPreview {
	if seed.UtilizationGiB <= 0 {
		seed.UtilizationGiB = 5.0
	}
	if len(seed.Tiers) == 0 {
		seed = DefaultSeed(seed.UtilizationGiB)
	}

	target := int64(seed.UtilizationGiB * float64(GiB))
	prev := SeedPreview{
		UtilizationBytes: target,
		UtilizationGiB:   seed.UtilizationGiB,
		OK:               true,
	}

	var tierSum int64
	for i, t := range seed.Tiers {
		tb := t.TierBudgetBytes()
		comp := t.CompositionBytesPerNamespace()
		head := t.BytesPerNamespace - comp
		usedPct := 0.0
		if t.BytesPerNamespace > 0 {
			usedPct = 100 * float64(comp) / float64(t.BytesPerNamespace)
		}
		fits := t.BytesPerNamespace <= 0 || comp <= t.BytesPerNamespace
		if t.BytesPerNamespace > 0 && !fits {
			prev.OK = false
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: "error", Tier: t.Name, Code: "composition_overflow",
				Message: fmt.Sprintf("%s composition uses %s per namespace but budget is %s — reduce counts or SmallX/LargeX",
					t.Name, human(comp), human(t.BytesPerNamespace)),
			})
		} else if usedPct > 95 {
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: "warning", Tier: t.Name, Code: "composition_tight",
				Message: fmt.Sprintf("%s composition uses %.0f%% of per-namespace budget", t.Name, usedPct),
			})
		}

		for _, k := range t.Composition {
			if !k.Enabled {
				continue
			}
			if k.SmallX > k.LargeX {
				prev.OK = false
				prev.Issues = append(prev.Issues, ValidationIssue{
					Level: "error", Tier: t.Name, Kind: string(k.Kind), Code: "range_inverted",
					Message: fmt.Sprintf("%s/%s: SmallX (%s) > LargeX (%s)", t.Name, k.Kind, human(int64(k.SmallX)), human(int64(k.LargeX))),
				})
			}
			if k.RecordsPerNamespace > 0 && t.BytesPerNamespace > 0 && k.BytesPerNamespace() > t.BytesPerNamespace {
				prev.OK = false
				maxRec := MaxRecordsForBudget(t.BytesPerNamespace, k.AvgBytes())
				prev.Issues = append(prev.Issues, ValidationIssue{
					Level: "error", Tier: t.Name, Kind: string(k.Kind), Code: "kind_overflow",
					Message: fmt.Sprintf("%s/%s: %d×%s = %s exceeds namespace budget %s (max ~%d records at this avg size)",
						t.Name, k.Kind, k.RecordsPerNamespace, human(int64(k.AvgBytes())),
						human(k.BytesPerNamespace()), human(t.BytesPerNamespace), maxRec),
				})
			}
		}

		others := tierSumExcluding(seed.Tiers, i)
		remain := target - others
		if remain < 0 {
			remain = 0
		}
		maxBytes := int64(0)
		maxNS := 0
		if t.NamespaceCount > 0 {
			maxBytes = remain / int64(t.NamespaceCount)
		}
		if t.BytesPerNamespace > 0 {
			maxNS = int(remain / t.BytesPerNamespace)
		}

		pctUtil := 0.0
		if target > 0 {
			pctUtil = 100 * float64(tb) / float64(target)
		}
		prev.Tiers = append(prev.Tiers, TierPreview{
			Name:                         t.Name,
			NamespaceCount:               t.NamespaceCount,
			BytesPerNamespace:            t.BytesPerNamespace,
			TierBudgetBytes:              tb,
			CompositionBytesPerNamespace: comp,
			CompositionHeadroomBytes:     head,
			CompositionUsedPct:           usedPct,
			Fits:                         fits,
			PercentOfUtilization:         pctUtil,
			MaxBytesPerNamespace:         maxBytes,
			MaxNamespaceCount:            maxNS,
		})
		tierSum += tb
	}

	prev.TierBudgetsTotalBytes = tierSum
	prev.TierBudgetsTotalGiB = float64(tierSum) / float64(GiB)
	prev.BudgetDeltaBytes = tierSum - target
	if target > 0 {
		prev.BudgetDeltaPct = 100 * float64(prev.BudgetDeltaBytes) / float64(target)
	}

	tol := tolerancePercent
	if tol < 0 {
		tol = 0
	}
	absPct := math.Abs(prev.BudgetDeltaPct)

	if tol > 0 {
		if absPct > tol {
			prev.OK = false
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: "error", Code: "tier_sum_out_of_tolerance",
				Message: fmt.Sprintf("tier budgets sum to %.3f GiB vs utilization %.3f GiB (Δ %+.1f%%) — outside ±%.0f%% tolerance",
					prev.TierBudgetsTotalGiB, seed.UtilizationGiB, prev.BudgetDeltaPct, tol),
			})
		} else if absPct > 2 {
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: "warning", Code: "tier_sum_near_edge",
				Message: fmt.Sprintf("tier budgets Δ %+.1f%% within ±%.0f%% tolerance", prev.BudgetDeltaPct, tol),
			})
		}
	} else {
		// Strict: any meaningful overshoot is illegal.
		if prev.BudgetDeltaBytes > int64(MiB) {
			prev.OK = false
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: "error", Code: "tier_sum_overshoot",
				Message: fmt.Sprintf("impossible: tier budgets sum to %.3f GiB which exceeds utilization target %.3f GiB by %s (Δ %+.1f%%)",
					prev.TierBudgetsTotalGiB, seed.UtilizationGiB, human(prev.BudgetDeltaBytes), prev.BudgetDeltaPct),
			})
		} else if prev.BudgetDeltaBytes < -int64(50*MiB) {
			level := "warning"
			if absPct > 10 {
				level = "error"
				prev.OK = false
			}
			prev.Issues = append(prev.Issues, ValidationIssue{
				Level: level, Code: "tier_sum_undershoot",
				Message: fmt.Sprintf("tier budgets sum to %.3f GiB but utilization target is %.3f GiB (short by %s)",
					prev.TierBudgetsTotalGiB, seed.UtilizationGiB, human(-prev.BudgetDeltaBytes)),
			})
		}
	}

	return prev
}

func tierSumExcluding(tiers []TierSeed, exclude int) int64 {
	var sum int64
	for i, t := range tiers {
		if i == exclude {
			continue
		}
		sum += t.TierBudgetBytes()
	}
	return sum
}

// ClampToFeasible forces a seed into a legal shape:
//   - each tier's N×size/ns cannot exceed remaining utilization budget
//   - each kind's records/ns cannot exceed the per-namespace budget
//
// Returns the clamped seed and whether anything changed.
func ClampToFeasible(seed SeedConfig) (SeedConfig, bool) {
	if seed.UtilizationGiB <= 0 {
		seed.UtilizationGiB = 5.0
	}
	target := int64(seed.UtilizationGiB * float64(GiB))
	changed := false

	for i := range seed.Tiers {
		t := &seed.Tiers[i]
		if t.NamespaceCount < 1 {
			t.NamespaceCount = 1
			changed = true
		}
		others := tierSumExcluding(seed.Tiers, i)
		remain := target - others
		if remain < int64(MiB) {
			remain = int64(MiB) // leave at least 1 MiB so the tier isn't zeroed silently
		}
		maxBytes := remain / int64(t.NamespaceCount)
		if maxBytes < int64(MiB) {
			maxBytes = int64(MiB)
			// Still too big on ns count — reduce namespaces to fit at 1 MiB each.
			maxNS := int(remain / int64(MiB))
			if maxNS < 1 {
				maxNS = 1
			}
			if t.NamespaceCount > maxNS {
				t.NamespaceCount = maxNS
				changed = true
			}
			maxBytes = remain / int64(t.NamespaceCount)
			if maxBytes < 1 {
				maxBytes = 1
			}
		}
		if t.BytesPerNamespace > maxBytes {
			t.BytesPerNamespace = maxBytes
			changed = true
		}
		if t.BytesPerNamespace < 1 {
			t.BytesPerNamespace = 1
			changed = true
		}

		// Clamp composition into the (possibly reduced) per-ns budget.
		for {
			used := t.CompositionBytesPerNamespace()
			if used <= t.BytesPerNamespace {
				break
			}
			shrunk := false
			// Prefer shrinking the largest consumer first.
			best := -1
			var bestBytes int64
			for j, k := range t.Composition {
				if !k.Enabled || k.RecordsPerNamespace <= 0 {
					continue
				}
				b := k.BytesPerNamespace()
				if b > bestBytes {
					bestBytes = b
					best = j
				}
			}
			if best < 0 {
				break
			}
			t.Composition[best].RecordsPerNamespace--
			if t.Composition[best].RecordsPerNamespace <= 0 {
				t.Composition[best].RecordsPerNamespace = 0
				t.Composition[best].Enabled = false
			}
			changed = true
			shrunk = true
			if !shrunk {
				break
			}
		}
	}
	return seed, changed
}

// MaxRecordsForBudget returns how many records fit in budget at avg size.
func MaxRecordsForBudget(budgetBytes int64, avgBytes int) int {
	if avgBytes <= 0 || budgetBytes <= 0 {
		return 0
	}
	return int(budgetBytes / int64(avgBytes))
}
