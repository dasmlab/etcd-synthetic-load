package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Save writes the plan to plansDir/<id>.yaml.
func Save(p *Plan, plansDir string) (string, error) {
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(plansDir, p.Metadata.ID+".yaml")
	b, err := yaml.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Load reads a plan by id or by absolute/relative path.
func Load(plansDir, idOrPath string) (*Plan, error) {
	path := idOrPath
	if !strings.HasSuffix(idOrPath, ".yaml") && !strings.HasSuffix(idOrPath, ".yml") && !filepath.IsAbs(idOrPath) {
		if _, err := os.Stat(idOrPath); err != nil {
			path = filepath.Join(plansDir, idOrPath+".yaml")
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Metadata.ID == "" {
		return nil, fmt.Errorf("plan %s missing metadata.id", path)
	}
	return &p, nil
}

// List returns all plans in plansDir (newest first by filename).
func List(plansDir string) ([]*Plan, error) {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Plan
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		p, err := Load(plansDir, strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"))
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
