package compose

import (
	"strings"
	"testing"
)

func TestDefaultSeedPreviewOK(t *testing.T) {
	seed := DefaultSeed(5.0)
	prev := Preview(seed)
	if !prev.OK {
		t.Fatalf("default seed should be OK, issues=%v", prev.Issues)
	}
	if abs(prev.BudgetDeltaPct) > 2 {
		t.Fatalf("budget delta %.2f%% too large", prev.BudgetDeltaPct)
	}
	for _, tp := range prev.Tiers {
		if !tp.Fits {
			t.Fatalf("tier %s composition does not fit", tp.Name)
		}
	}
}

func TestOvershootImpossible(t *testing.T) {
	gib := 5.0
	seed := DefaultSeed(gib)
	// 40 × 250 MiB ≈ 9.8 GiB — cannot fit under 5 GiB with other tiers.
	for i := range seed.Tiers {
		if seed.Tiers[i].Name == "MEDIUM" {
			seed.Tiers[i].NamespaceCount = 40
			seed.Tiers[i].BytesPerNamespace = 250 * MiB
		}
	}
	prev := Preview(seed)
	if prev.OK {
		t.Fatal("40×250MiB under 5GiB target must not be OK")
	}
	clamped, changed := ClampToFeasible(seed)
	if !changed {
		t.Fatal("expected ClampToFeasible to shrink overshoot")
	}
	prev2 := Preview(clamped)
	if !prev2.OK {
		t.Fatalf("clamped seed should be OK, issues=%v", prev2.Issues)
	}
	for _, tp := range prev2.Tiers {
		if tp.Name == "MEDIUM" && tp.BytesPerNamespace >= 250*MiB {
			t.Fatalf("MEDIUM size/ns should have been clamped below 250MiB, got %d", tp.BytesPerNamespace)
		}
	}
}

func TestImpossibleCompositionClampedOnGenerate(t *testing.T) {
	seed := DefaultSeed(5.0)
	for i := range seed.Tiers {
		if seed.Tiers[i].Name != "SMALL" {
			continue
		}
		for j := range seed.Tiers[i].Composition {
			if seed.Tiers[i].Composition[j].Kind == KindConfigMaps {
				seed.Tiers[i].Composition[j].Enabled = true
				seed.Tiers[i].Composition[j].RecordsPerNamespace = 10000
				seed.Tiers[i].Composition[j].SmallX = 100 * KiB
				seed.Tiers[i].Composition[j].LargeX = 100 * KiB
			}
		}
	}
	prev := Preview(seed)
	if prev.OK {
		t.Fatal("expected impossible composition to fail preview before clamp")
	}
	plan, err := Generate(GenerateInput{Seed: &seed, UtilizationGiB: &seed.UtilizationGiB})
	if err != nil {
		t.Fatalf("Generate should clamp then succeed: %v", err)
	}
	for _, tier := range plan.Tiers {
		if tier.Name != "SMALL" {
			continue
		}
		for _, c := range tier.Composition {
			if c.Kind == KindConfigMaps && c.RecordCount/tier.NamespaceCount >= 10000 {
				t.Fatalf("configmaps/ns should have been clamped below 10000, got %d", c.RecordCount/tier.NamespaceCount)
			}
		}
		if tier.TotalSizeBytes > tier.TierBudgetBytes+int64(MiB) {
			t.Fatalf("SMALL total size %d exceeds tier budget %d", tier.TotalSizeBytes, tier.TierBudgetBytes)
		}
	}
}

func TestGenerateDefaultSeed(t *testing.T) {
	p, err := Generate(GenerateInput{Name: "test", ClusterDisplayName: "PROD-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tiers) != 3 {
		t.Fatalf("want 3 tiers, got %d", len(p.Tiers))
	}
	report := FormatReport(p)
	for _, want := range []string{"SMALL", "MEDIUM", "LARGE", "tier budget", "SmallX", "LargeX"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
