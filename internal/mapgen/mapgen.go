// Package mapgen materializes a Generation Seed into a sharded load map
// suitable for paced / parallel apply (never one giant file).
package mapgen

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
)

const (
	// DefaultObjectsPerShard keeps shards small enough for controlled load workers.
	DefaultObjectsPerShard = 500
)

// ObjectSpec is one create operation in a shard.
type ObjectSpec struct {
	Kind      string `yaml:"kind" json:"kind"`
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
	Tier      string `yaml:"tier" json:"tier"`
	SizeBytes int    `yaml:"sizeBytes" json:"sizeBytes"`
}

// ShardFile is one parallelizable batch.
type ShardFile struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   ShardMeta    `yaml:"metadata"`
	Objects    []ObjectSpec `yaml:"objects"`
}

type ShardMeta struct {
	ShardID   string `yaml:"shardId"`
	TargetID  string `yaml:"targetId"`
	Tier      string `yaml:"tier"`
	Index     int    `yaml:"index"`
	Count     int    `yaml:"count"`
	CreatedAt string `yaml:"createdAt"`
}

// Manifest indexes all shards and records validation.
type Manifest struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   ManifestMeta  `yaml:"metadata" json:"metadata"`
	Summary    ManifestSum   `yaml:"summary" json:"summary"`
	Shards     []ShardRef    `yaml:"shards" json:"shards"`
	Validation ManifestValid `yaml:"validation" json:"validation"`
}

type ManifestMeta struct {
	TargetID  string `yaml:"targetId" json:"targetId"`
	CreatedAt string `yaml:"createdAt" json:"createdAt"`
}

type ManifestSum struct {
	TotalShards     int            `yaml:"totalShards" json:"totalShards"`
	TotalObjects    int            `yaml:"totalObjects" json:"totalObjects"`
	TotalNamespaces int            `yaml:"totalNamespaces" json:"totalNamespaces"`
	TotalSizeBytes  int64          `yaml:"totalSizeBytes" json:"totalSizeBytes"`
	TotalSizeGiB    float64        `yaml:"totalSizeGiB" json:"totalSizeGiB"`
	ByKind          map[string]int `yaml:"byKind" json:"byKind"`
	ByTier          map[string]int `yaml:"byTier" json:"byTier"`
}

type ShardRef struct {
	Path    string `yaml:"path" json:"path"`
	Tier    string `yaml:"tier" json:"tier"`
	Index   int    `yaml:"index" json:"index"`
	Objects int    `yaml:"objects" json:"objects"`
}

type ManifestValid struct {
	OK      bool     `yaml:"ok" json:"ok"`
	Message string   `yaml:"message,omitempty" json:"message,omitempty"`
	Issues  []string `yaml:"issues,omitempty" json:"issues,omitempty"`
}

// Generate writes a sharded map under targetDir/map/ from seed.
func Generate(targetID, targetDir string, seed compose.SeedConfig, objectsPerShard int) (*Manifest, error) {
	if objectsPerShard <= 0 {
		objectsPerShard = DefaultObjectsPerShard
	}
	mapDir := filepath.Join(targetDir, "map")
	shardDir := filepath.Join(mapDir, "shards")
	_ = os.RemoveAll(shardDir)
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		return nil, err
	}

	seed, _ = compose.ClampToFeasible(seed)
	plan, err := compose.Generate(compose.GenerateInput{
		Name:               targetID,
		ClusterDisplayName: targetID,
		UtilizationGiB:     &seed.UtilizationGiB,
		Seed:               &seed,
	})
	if err != nil {
		return nil, err
	}

	man := &Manifest{
		APIVersion: "etcd-synthetic-load.dasmlab.org/v1",
		Kind:       "LoadMapManifest",
		Metadata: ManifestMeta{
			TargetID:  targetID,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Summary: ManifestSum{
			ByKind: map[string]int{},
			ByTier: map[string]int{},
		},
	}

	prefix := "esl"
	var all []ObjectSpec
	nsSet := map[string]struct{}{}

	for _, tier := range plan.Tiers {
		tierKey := strings.ToLower(tier.Name)
		for i := 0; i < tier.NamespaceCount; i++ {
			ns := fmt.Sprintf("%s-%s-%04d", prefix, tierKey, i+1)
			nsSet[ns] = struct{}{}
			for _, c := range tier.Composition {
				perNS := c.RecordCount / tier.NamespaceCount
				rem := c.RecordCount % tier.NamespaceCount
				n := perNS
				if i < rem {
					n++
				}
				for j := 0; j < n; j++ {
					sz := c.AvgBytes
					if c.SizeRange.LargeX > c.SizeRange.SmallX {
						sz = c.SizeRange.SmallX + rand.Intn(c.SizeRange.LargeX-c.SizeRange.SmallX+1)
					}
					all = append(all, ObjectSpec{
						Kind:      string(c.Kind),
						Namespace: ns,
						Name:      fmt.Sprintf("esl-%s-%06d", short(c.Kind), j+1),
						Tier:      tier.Name,
						SizeBytes: sz,
					})
				}
			}
		}
	}

	// Write shards by tier for cleaner parallel workers.
	byTier := map[string][]ObjectSpec{}
	for _, o := range all {
		byTier[o.Tier] = append(byTier[o.Tier], o)
		man.Summary.ByKind[o.Kind]++
		man.Summary.ByTier[o.Tier]++
		man.Summary.TotalSizeBytes += int64(o.SizeBytes)
	}
	man.Summary.TotalObjects = len(all)
	man.Summary.TotalNamespaces = len(nsSet)
	man.Summary.TotalSizeGiB = float64(man.Summary.TotalSizeBytes) / float64(compose.GiB)

	shardIdx := 0
	for _, tierName := range []string{"SMALL", "MEDIUM", "LARGE"} {
		objs := byTier[tierName]
		if len(objs) == 0 {
			continue
		}
		for start := 0; start < len(objs); start += objectsPerShard {
			end := start + objectsPerShard
			if end > len(objs) {
				end = len(objs)
			}
			chunk := objs[start:end]
			sid := fmt.Sprintf("%s-%04d", strings.ToLower(tierName), shardIdx)
			rel := filepath.Join("shards", sid+".yaml")
			sf := ShardFile{
				APIVersion: "etcd-synthetic-load.dasmlab.org/v1",
				Kind:       "LoadMapShard",
				Metadata: ShardMeta{
					ShardID:   sid,
					TargetID:  targetID,
					Tier:      tierName,
					Index:     shardIdx,
					Count:     len(chunk),
					CreatedAt: man.Metadata.CreatedAt,
				},
				Objects: chunk,
			}
			b, err := yaml.Marshal(sf)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(mapDir, rel), b, 0o644); err != nil {
				return nil, err
			}
			man.Shards = append(man.Shards, ShardRef{Path: rel, Tier: tierName, Index: shardIdx, Objects: len(chunk)})
			shardIdx++
		}
	}
	man.Summary.TotalShards = len(man.Shards)

	man.Validation = validateManifest(man, plan)
	b, err := yaml.Marshal(man)
	if err != nil {
		return nil, err
	}
	header := "# Load map manifest — sharded for paced/parallel load\n# WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.\n\n"
	if err := os.WriteFile(filepath.Join(mapDir, "manifest.yaml"), append([]byte(header), b...), 0o644); err != nil {
		return nil, err
	}
	if !man.Validation.OK {
		return man, fmt.Errorf("map validation failed: %s", man.Validation.Message)
	}
	// Also keep a compact plan snapshot for UI report.
	pb, _ := yaml.Marshal(plan)
	_ = os.WriteFile(filepath.Join(mapDir, "plan.yaml"), pb, 0o644)
	return man, nil
}

func validateManifest(man *Manifest, plan *compose.Plan) ManifestValid {
	v := ManifestValid{OK: true}
	var issues []string
	if man.Summary.TotalObjects == 0 {
		issues = append(issues, "map has zero objects")
	}
	if man.Summary.TotalShards == 0 {
		issues = append(issues, "map has zero shards")
	}
	expected := plan.Summary.TotalRecords
	delta := man.Summary.TotalObjects - expected
	if delta < 0 {
		delta = -delta
	}
	// Allow tiny rounding differences across ns distribution.
	if expected > 0 && float64(delta)/float64(expected) > 0.01 {
		issues = append(issues, fmt.Sprintf("object count %d diverges from plan %d by more than 1%%", man.Summary.TotalObjects, expected))
	}
	sumShard := 0
	for _, s := range man.Shards {
		sumShard += s.Objects
	}
	if sumShard != man.Summary.TotalObjects {
		issues = append(issues, fmt.Sprintf("shard object sum %d != manifest total %d", sumShard, man.Summary.TotalObjects))
	}
	if len(issues) > 0 {
		v.OK = false
		v.Issues = issues
		v.Message = strings.Join(issues, "; ")
	} else {
		v.Message = "map constitution OK"
	}
	return v
}

func short(k compose.ObjectKind) string {
	switch k {
	case compose.KindSecrets:
		return "sec"
	case compose.KindConfigMaps:
		return "cm"
	case compose.KindServices:
		return "svc"
	case compose.KindRoutes:
		return "rt"
	case compose.KindEgressFirewalls:
		return "eg"
	case compose.KindRoleBindings:
		return "rb"
	case compose.KindServiceAccounts:
		return "sa"
	default:
		return "obj"
	}
}

// LoadManifest reads map/manifest.yaml from a target dir.
func LoadManifest(targetDir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(targetDir, "map", "manifest.yaml"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
