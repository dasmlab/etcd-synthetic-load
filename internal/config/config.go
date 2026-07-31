package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// RuntimeConfig is the static YAML (cluster display, paths, pacing).
type RuntimeConfig struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name  string `yaml:"name"`
		Notes string `yaml:"notes,omitempty"`
	} `yaml:"metadata"`
	Cluster ClusterConfig `yaml:"cluster"`
	Paths   PathsConfig   `yaml:"paths"`
	Load    LoadPacing    `yaml:"load"`
	Server  ServerConfig  `yaml:"server"`
}

type ClusterConfig struct {
	DisplayName string `yaml:"displayName"`
	APIServer   string `yaml:"apiServer"`
	Context     string `yaml:"context"`
}

type PathsConfig struct {
	DataDir  string `yaml:"dataDir"`
	PlansDir string `yaml:"plansDir"`
	RunsDir  string `yaml:"runsDir"`
}

type LoadPacing struct {
	BatchSize           int      `yaml:"batchSize"`
	Concurrency         int      `yaml:"concurrency"`
	PauseBetweenBatches Duration `yaml:"pauseBetweenBatches"`
	NamespacePause      Duration `yaml:"namespacePause"`
}

// Duration wraps time.Duration for YAML strings like "500ms".
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		var n int64
		if err2 := value.Decode(&n); err2 == nil {
			*d = Duration(time.Duration(n))
			return nil
		}
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

type ServerConfig struct {
	ListenAddr string `yaml:"listenAddr"`
}

// LoadTarget is the dynamic YAML (utilization + seed tiers).
type LoadTarget struct {
	APIVersion  string            `yaml:"apiVersion"`
	Kind        string            `yaml:"kind"`
	Metadata    TargetMeta        `yaml:"metadata"`
	Utilization UtilizationConfig `yaml:"utilization"`
	Objects     ObjectTotals      `yaml:"objects"` // legacy; ignored when seedTiers set
	SeedTiers   []SeedTierYAML    `yaml:"seedTiers,omitempty"`
	// Deprecated shape overrides — prefer seedTiers.
	TierOverrides map[string]TierOverride `yaml:"tiers,omitempty"`
}

type SeedTierYAML struct {
	Name              string         `yaml:"name"`
	NamespaceCount    int            `yaml:"namespaceCount"`
	BytesPerNamespace int64          `yaml:"bytesPerNamespace"`
	Composition       []KindSpecYAML `yaml:"composition"`
}

type KindSpecYAML struct {
	Kind                string `yaml:"kind"`
	Enabled             bool   `yaml:"enabled"`
	RecordsPerNamespace int    `yaml:"recordsPerNamespace"`
	SmallX              int    `yaml:"smallX"`
	LargeX              int    `yaml:"largeX"`
}

type TargetMeta struct {
	Name  string `yaml:"name"`
	Notes string `yaml:"notes,omitempty"`
}

type UtilizationConfig struct {
	TargetGiB            *float64 `yaml:"targetGiB"`
	TargetPercentOfQuota *float64 `yaml:"targetPercentOfQuota"`
	AssumedQuotaGiB      *float64 `yaml:"assumedQuotaGiB"`
}

type ObjectTotals struct {
	Secrets         *int `yaml:"secrets"`
	ConfigMaps      *int `yaml:"configmaps"`
	Services        *int `yaml:"services"`
	Routes          *int `yaml:"routes"`
	EgressFirewalls *int `yaml:"egressfirewalls"`
	RoleBindings    *int `yaml:"rolebindings"`
	ServiceAccounts *int `yaml:"serviceaccounts"`
}

type TierOverride struct {
	NamespaceCount *int     `yaml:"namespaceCount"`
	RecordShare    *float64 `yaml:"recordShare"`
	SizeShare      *float64 `yaml:"sizeShare"`
}

func DefaultRuntime() RuntimeConfig {
	return RuntimeConfig{
		APIVersion: "etcd-synthetic-load.dasmlab.org/v1",
		Kind:       "RuntimeConfig",
		Cluster: ClusterConfig{
			DisplayName: "default",
			APIServer:   envOr("OC_SERVER", ""),
		},
		Paths: PathsConfig{
			DataDir:  envOr("ESL_DATA_DIR", "./data"),
			PlansDir: envOr("ESL_DATA_DIR", "./data") + "/plans",
			RunsDir:  envOr("ESL_DATA_DIR", "./data") + "/runs",
		},
		Load: LoadPacing{
			BatchSize:           50,
			Concurrency:         8,
			PauseBetweenBatches: Duration(500 * time.Millisecond),
			NamespacePause:      Duration(200 * time.Millisecond),
		},
		Server: ServerConfig{ListenAddr: ":8080"},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadRuntime(path string) (*RuntimeConfig, error) {
	cfg := DefaultRuntime()
	if path == "" {
		path = os.Getenv("ESL_RUNTIME_CONFIG")
	}
	if path == "" {
		applyEnvOverrides(&cfg)
		return &cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	applyEnvOverrides(&cfg)
	normalizePaths(&cfg)
	return &cfg, nil
}

func applyEnvOverrides(cfg *RuntimeConfig) {
	if v := os.Getenv("OC_SERVER"); v != "" {
		cfg.Cluster.APIServer = v
	}
	if v := os.Getenv("ESL_DATA_DIR"); v != "" {
		cfg.Paths.DataDir = v
		cfg.Paths.PlansDir = v + "/plans"
		cfg.Paths.RunsDir = v + "/runs"
	}
}

func normalizePaths(cfg *RuntimeConfig) {
	if cfg.Paths.DataDir == "" {
		cfg.Paths.DataDir = "./data"
	}
	if cfg.Paths.PlansDir == "" {
		cfg.Paths.PlansDir = cfg.Paths.DataDir + "/plans"
	}
	if cfg.Paths.RunsDir == "" {
		cfg.Paths.RunsDir = cfg.Paths.DataDir + "/runs"
	}
	if cfg.Load.BatchSize <= 0 {
		cfg.Load.BatchSize = 50
	}
	if cfg.Load.Concurrency <= 0 {
		cfg.Load.Concurrency = 8
	}
	if cfg.Server.ListenAddr == "" {
		cfg.Server.ListenAddr = ":8080"
	}
}

func LoadTargetFile(path string) (*LoadTarget, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t LoadTarget
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func EnsureDirs(cfg *RuntimeConfig) error {
	for _, d := range []string{cfg.Paths.DataDir, cfg.Paths.PlansDir, cfg.Paths.RunsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}
