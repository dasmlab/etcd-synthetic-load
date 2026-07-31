package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State is a load/test run persisted under runsDir.
type State struct {
	ID        string         `json:"id"`
	PlanID    string         `json:"planId"`
	TargetID  string         `json:"targetId,omitempty"`
	Kind      string         `json:"kind"`  // load | test
	State     string         `json:"state"` // pending|running|complete|failed
	DryRun    bool           `json:"dryRun"`
	Message   string         `json:"message"`
	Progress  float64        `json:"progress"`
	Done      int64          `json:"done"`
	Total     int64          `json:"total"`
	Created   map[string]int `json:"created,omitempty"`
	Existing  map[string]int `json:"existing,omitempty"`
	Skipped   map[string]int `json:"skipped,omitempty"`
	Errors    []string       `json:"errors,omitempty"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
}

type Store struct {
	dir string
	mu  sync.Mutex
	mem map[string]*State
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, mem: map[string]*State{}}
	_ = s.loadDisk()
	return s, nil
}

func (s *Store) loadDisk() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var st State
		if json.Unmarshal(b, &st) == nil && st.ID != "" {
			s.mem[st.ID] = &st
		}
	}
	return nil
}

func (s *Store) Put(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *st
	s.mem[st.ID] = &cp
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, st.ID+".json"), b, 0o644)
}

func (s *Store) Get(id string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.mem[id]
	if !ok {
		return nil, fmt.Errorf("run %s not found", id)
	}
	cp := *st
	return &cp, nil
}

func (s *Store) List() []*State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*State, 0, len(s.mem))
	for _, st := range s.mem {
		cp := *st
		out = append(out, &cp)
	}
	return out
}

func NewID(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, time.Now().UTC().Format("20060102T150405Z"))
}
