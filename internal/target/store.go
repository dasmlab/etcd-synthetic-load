package target

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
)

// Status is the workflow stage for a Target.
type Status string

const (
	StatusCreated    Status = "created"
	StatusConfigured Status = "configured"
	StatusGenerated  Status = "generated"
	StatusLoading    Status = "loading"
	StatusLoaded     Status = "loaded"
	StatusTesting    Status = "testing"
	StatusReported   Status = "reported"
)

// Target is a first-class runtime object (cluster under test).
// Credentials are NEVER written here — use OC_USER/OC_PASSWORD or KUBECONFIG.
type Target struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Metadata   TargetMeta `yaml:"metadata" json:"metadata"`
	Spec       Spec       `yaml:"spec" json:"spec"`
	Status     StatusInfo `yaml:"status" json:"status"`
}

type TargetMeta struct {
	ID        string    `yaml:"id" json:"id"`
	Name      string    `yaml:"name" json:"name"`
	CreatedAt time.Time `yaml:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `yaml:"updatedAt" json:"updatedAt"`
	Notes     string    `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type Spec struct {
	DisplayName string `yaml:"displayName" json:"displayName"`
	APIServer   string `yaml:"apiServer" json:"apiServer"`
	// Username is optional display/login hint; password never stored.
	Username    string `yaml:"username,omitempty" json:"username,omitempty"`
	PasswordRef string `yaml:"passwordRef,omitempty" json:"passwordRef,omitempty"` // e.g. env:OC_PASSWORD
	Context     string `yaml:"context,omitempty" json:"context,omitempty"`
	// TolerancePercent is ± allowed deviation of tier budgets vs utilization (default 10).
	TolerancePercent float64 `yaml:"tolerancePercent" json:"tolerancePercent"`
}

type StatusInfo struct {
	Phase     Status `yaml:"phase" json:"phase"`
	Message   string `yaml:"message,omitempty" json:"message,omitempty"`
	PlanID    string `yaml:"planId,omitempty" json:"planId,omitempty"`
	MapReady  bool   `yaml:"mapReady" json:"mapReady"`
	Loaded    bool   `yaml:"loaded" json:"loaded"`
	LastError string `yaml:"lastError,omitempty" json:"lastError,omitempty"`
}

// Store persists targets under dataDir/targets/<id>/.
type Store struct {
	Root string // .../targets
}

func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "targets")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) Dir(id string) string {
	return filepath.Join(s.Root, id)
}

func (s *Store) Create(displayName, apiServer, username string, tolerance float64) (*Target, error) {
	if displayName == "" {
		return nil, fmt.Errorf("displayName required")
	}
	if apiServer == "" {
		return nil, fmt.Errorf("apiServer required")
	}
	if tolerance <= 0 {
		tolerance = 10
	}
	id, err := newID(displayName)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &Target{
		APIVersion: "etcd-synthetic-load.dasmlab.org/v1",
		Kind:       "Target",
		Metadata: TargetMeta{
			ID:        id,
			Name:      displayName,
			CreatedAt: now,
			UpdatedAt: now,
			Notes:     "WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT",
		},
		Spec: Spec{
			DisplayName:      displayName,
			APIServer:        apiServer,
			Username:         username,
			PasswordRef:      "env:OC_PASSWORD",
			TolerancePercent: tolerance,
		},
		Status: StatusInfo{Phase: StatusCreated, Message: "target created — configure seed next"},
	}
	if err := os.MkdirAll(s.Dir(id), 0o755); err != nil {
		return nil, err
	}
	for _, sub := range []string{"map/shards", "runs", "reports"} {
		if err := os.MkdirAll(filepath.Join(s.Dir(id), sub), 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.Save(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) Save(t *Target) error {
	t.Metadata.UpdatedAt = time.Now().UTC()
	b, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	header := "# Target — credentials are NOT stored here.\n# Use OC_SERVER / OC_USER / OC_PASSWORD or KUBECONFIG.\n# WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.\n\n"
	return os.WriteFile(filepath.Join(s.Dir(t.Metadata.ID), "target.yaml"), append([]byte(header), b...), 0o644)
}

func (s *Store) Get(id string) (*Target, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir(id), "target.yaml"))
	if err != nil {
		return nil, err
	}
	var t Target
	if err := yaml.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) List() ([]*Target, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Target
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := s.Get(e.Name())
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) SaveSeed(id string, seed compose.SeedConfig) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	tol := t.Spec.TolerancePercent
	if tol <= 0 {
		tol = 10
	}
	seed, _ = compose.ClampToFeasible(seed)
	prev := compose.PreviewWithTolerance(seed, tol)
	if !prev.OK {
		msgs := []string{}
		for _, iss := range prev.Issues {
			if iss.Level == "error" {
				msgs = append(msgs, iss.Message)
			}
		}
		return fmt.Errorf("seed validation failed (±%.0f%%): %s", tol, strings.Join(msgs, "; "))
	}
	b, err := yaml.Marshal(seed)
	if err != nil {
		return err
	}
	header := "# Generation Seed for this Target\n# WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.\n\n"
	if err := os.WriteFile(filepath.Join(s.Dir(id), "seed.yaml"), append([]byte(header), b...), 0o644); err != nil {
		return err
	}
	t.Status.Phase = StatusConfigured
	t.Status.Message = "seed saved and validated"
	t.Status.LastError = ""
	return s.Save(t)
}

func (s *Store) LoadSeed(id string) (*compose.SeedConfig, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir(id), "seed.yaml"))
	if err != nil {
		return nil, err
	}
	var seed compose.SeedConfig
	if err := yaml.Unmarshal(b, &seed); err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) Delete(id string) error {
	return os.RemoveAll(s.Dir(id))
}

func (s *Store) SetPhase(id string, phase Status, msg string) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	t.Status.Phase = phase
	t.Status.Message = msg
	if phase == StatusLoaded {
		t.Status.Loaded = true
	}
	if phase == StatusGenerated {
		t.Status.MapReady = true
	}
	return s.Save(t)
}

func newID(name string) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, slug)
	return fmt.Sprintf("%s-%s", slug, hex.EncodeToString(b[:])), nil
}
