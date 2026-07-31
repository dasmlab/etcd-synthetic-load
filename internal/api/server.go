package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
	"github.com/dasmlab/etcd-synthetic-load/internal/k8sclient"
	"github.com/dasmlab/etcd-synthetic-load/internal/loadgen"
	"github.com/dasmlab/etcd-synthetic-load/internal/mapgen"
	"github.com/dasmlab/etcd-synthetic-load/internal/runs"
	"github.com/dasmlab/etcd-synthetic-load/internal/target"
)

type Server struct {
	runtime  *config.RuntimeConfig
	runs     *runs.Store
	targets  *target.Store
	router   chi.Router
	buildVer string
	static   http.Handler
}

func New(rt *config.RuntimeConfig, runStore *runs.Store, targetStore *target.Store, buildVer string, static http.Handler) *Server {
	s := &Server{runtime: rt, runs: runStore, targets: targetStore, buildVer: buildVer, static: static}
	s.router = s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		MaxAge:         300,
	}))

	r.Get("/healthz", s.healthz)
	r.Get("/isalive", s.healthz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.healthz)
		r.Get("/version", s.version)
		r.Get("/runtime", s.getRuntime)
		r.Get("/seed/defaults", s.seedDefaults)
		r.Get("/seed/kinds", s.seedKinds)
		r.Post("/seed/preview", s.seedPreview)
		r.Get("/targets", s.listTargets)
		r.Post("/targets", s.createTarget)
		r.Get("/targets/{id}", s.getTarget)
		r.Post("/targets/{id}/configure", s.configureTarget)
		r.Post("/targets/{id}/generate", s.generateTargetMap)
		r.Delete("/targets/{id}", s.deleteTarget)
		r.Post("/generate", s.generate)
		r.Get("/plans", s.listPlans)
		r.Get("/plans/{id}", s.getPlan)
		r.Post("/load", s.startLoad)
		r.Get("/load/{id}/status", s.loadStatus)
		r.Post("/test", s.startTest)
		r.Get("/results/{id}", s.getResults)
	})

	if s.static != nil {
		r.NotFound(s.static.ServeHTTP)
		r.Get("/*", s.static.ServeHTTP)
	}
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"warning": "NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT",
		"version": s.buildVer,
	})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.buildVer})
}

func (s *Server) getRuntime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime)
}

type targetView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	APIServer string `json:"apiServer"`
	Status    string `json:"status"`
	PlanID    string `json:"planId,omitempty"`
	MapReady  bool   `json:"mapReady"`
	Loaded    bool   `json:"loaded"`
	Message   string `json:"message,omitempty"`
}

func (s *Server) listTargets(w http.ResponseWriter, r *http.Request) {
	if s.targets == nil {
		writeJSON(w, http.StatusOK, []targetView{s.primaryTarget()})
		return
	}
	list, err := s.targets.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]targetView, 0, len(list))
	for _, t := range list {
		out = append(out, targetView{
			ID: t.Metadata.ID, Name: t.Spec.DisplayName, APIServer: t.Spec.APIServer,
			Status: string(t.Status.Phase), PlanID: t.Status.PlanID,
			MapReady: t.Status.MapReady, Loaded: t.Status.Loaded, Message: t.Status.Message,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getTarget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.targets == nil {
		t := s.primaryTarget()
		if id != t.ID && id != "default" {
			http.Error(w, "target not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, t)
		return
	}
	t, err := s.targets.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type createTargetReq struct {
	Name             string  `json:"name"`
	APIServer        string  `json:"apiServer"`
	Username         string  `json:"username"`
	TolerancePercent float64 `json:"tolerancePercent"`
}

func (s *Server) createTarget(w http.ResponseWriter, r *http.Request) {
	if s.targets == nil {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createTargetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.APIServer == "" {
		req.APIServer = s.runtime.Cluster.APIServer
	}
	t, err := s.targets.Create(req.Name, req.APIServer, req.Username, req.TolerancePercent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) configureTarget(w http.ResponseWriter, r *http.Request) {
	if s.targets == nil {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var seed compose.SeedConfig
	if err := json.NewDecoder(r.Body).Decode(&seed); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(seed.Tiers) == 0 {
		seed = compose.DefaultSeed(seed.UtilizationGiB)
	}
	if err := s.targets.SaveSeed(id, seed); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, _ := s.targets.Get(id)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) generateTargetMap(w http.ResponseWriter, r *http.Request) {
	if s.targets == nil {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	seed, err := s.targets.LoadSeed(id)
	if err != nil {
		http.Error(w, "configure target first: "+err.Error(), http.StatusBadRequest)
		return
	}
	man, err := mapgen.Generate(id, s.targets.Dir(id), *seed, mapgen.DefaultObjectsPerShard)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.targets.SetPhase(id, target.StatusGenerated, man.Validation.Message)
	writeJSON(w, http.StatusOK, man)
}

func (s *Server) deleteTarget(w http.ResponseWriter, r *http.Request) {
	if s.targets == nil {
		http.Error(w, "target store unavailable", http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.targets.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) primaryTarget() targetView {
	name := s.runtime.Cluster.DisplayName
	if name == "" {
		name = "default"
	}
	id := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	status := "idle"
	planID := ""
	plans, _ := compose.List(s.runtime.Paths.PlansDir)
	if len(plans) > 0 {
		planID = plans[0].Metadata.ID
		status = "generated"
	}
	return targetView{
		ID: id, Name: name, APIServer: s.runtime.Cluster.APIServer,
		Status: status, PlanID: planID,
	}
}

type generateReq struct {
	Name               string              `json:"name"`
	TargetID           string              `json:"targetId"`
	UtilizationGiB     *float64            `json:"utilizationGiB"`
	UtilizationPercent *float64            `json:"utilizationPercent"`
	AssumedQuotaGiB    float64             `json:"assumedQuotaGiB"`
	Objects            map[string]int      `json:"objects"`
	Seed               *compose.SeedConfig `json:"seed"`
}

func (s *Server) seedDefaults(w http.ResponseWriter, r *http.Request) {
	gib := 5.0
	if v := r.URL.Query().Get("utilizationGiB"); v != "" {
		fmt.Sscanf(v, "%f", &gib)
	}
	writeJSON(w, http.StatusOK, compose.DefaultSeed(gib))
}

func (s *Server) seedKinds(w http.ResponseWriter, r *http.Request) {
	kinds := make([]string, 0, len(compose.PickListKinds))
	for _, k := range compose.PickListKinds {
		kinds = append(kinds, string(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kinds":   kinds,
		"source":  "etcd-size-per-namespace defaults (+ routes)",
		"warning": "NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT",
	})
}

func (s *Server) seedPreview(w http.ResponseWriter, r *http.Request) {
	var seed compose.SeedConfig
	if err := json.NewDecoder(r.Body).Decode(&seed); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clamped, changed := compose.ClampToFeasible(seed)
	prev := compose.Preview(clamped)
	writeJSON(w, http.StatusOK, map[string]any{
		"preview": prev,
		"seed":    clamped,
		"clamped": changed,
	})
}

func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	var req generateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in := compose.GenerateInput{
		Name:               req.Name,
		ClusterDisplayName: s.runtime.Cluster.DisplayName,
		UtilizationGiB:     req.UtilizationGiB,
		UtilizationPercent: req.UtilizationPercent,
		AssumedQuotaGiB:    req.AssumedQuotaGiB,
		Objects:            map[compose.ObjectKind]int{},
		Seed:               req.Seed,
	}
	for k, v := range req.Objects {
		in.Objects[compose.ObjectKind(k)] = v
	}
	if req.Seed != nil && req.UtilizationGiB == nil && req.Seed.UtilizationGiB > 0 {
		g := req.Seed.UtilizationGiB
		in.UtilizationGiB = &g
	}
	plan, err := compose.Generate(in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := compose.Save(plan, s.runtime.Paths.PlansDir); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := compose.List(s.runtime.Paths.PlansDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	plan, err := compose.Load(s.runtime.Paths.PlansDir, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

type loadReq struct {
	PlanID  string `json:"planId"`
	Confirm bool   `json:"confirm"`
	DryRun  bool   `json:"dryRun"`
}

func (s *Server) startLoad(w http.ResponseWriter, r *http.Request) {
	var req loadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.PlanID == "" {
		http.Error(w, "planId required", http.StatusBadRequest)
		return
	}
	if !req.DryRun && !req.Confirm {
		http.Error(w, "confirm=true required for real load (Are you really sure?)", http.StatusBadRequest)
		return
	}
	plan, err := compose.Load(s.runtime.Paths.PlansDir, req.PlanID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	id := runs.NewID("load")
	st := &runs.State{
		ID:        id,
		PlanID:    req.PlanID,
		Kind:      "load",
		State:     "pending",
		DryRun:    req.DryRun,
		Message:   "queued",
		StartedAt: time.Now().UTC(),
	}
	_ = s.runs.Put(st)

	go s.runLoad(id, plan, req.DryRun, req.Confirm)
	writeJSON(w, http.StatusAccepted, st)
}

func (s *Server) runLoad(id string, plan *compose.Plan, dryRun, confirm bool) {
	update := func(mut func(*runs.State)) {
		st, err := s.runs.Get(id)
		if err != nil {
			return
		}
		mut(st)
		_ = s.runs.Put(st)
	}
	update(func(st *runs.State) {
		st.State = "running"
		st.Message = "starting"
	})

	var client kubernetes.Interface
	var dyn dynamic.Interface
	if !dryRun {
		cfg, err := k8sclient.BuildConfig(k8sclient.OptionsFromEnv(""))
		if err != nil {
			update(func(st *runs.State) {
				st.State = "failed"
				st.Message = err.Error()
				now := time.Now().UTC()
				st.EndedAt = &now
			})
			return
		}
		client, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			update(func(st *runs.State) {
				st.State = "failed"
				st.Message = err.Error()
				now := time.Now().UTC()
				st.EndedAt = &now
			})
			return
		}
		dyn, _ = dynamic.NewForConfig(cfg)
	}

	res, err := loadgen.RunPaced(context.Background(), loadgen.PacedOptions{
		Client:  client,
		Dynamic: dyn,
		Plan:    plan,
		Runtime: s.runtime,
		DryRun:  dryRun,
		Confirm: confirm || dryRun,
		ProgressFn: func(done, total int64, message string) {
			update(func(st *runs.State) {
				st.Done = done
				st.Total = total
				if total > 0 {
					st.Progress = float64(done) / float64(total)
				}
				st.Message = message
			})
		},
	})
	now := time.Now().UTC()
	if err != nil {
		update(func(st *runs.State) {
			st.State = "failed"
			st.Message = err.Error()
			st.EndedAt = &now
		})
		return
	}
	update(func(st *runs.State) {
		st.State = "complete"
		st.Message = "done"
		st.EndedAt = &now
		st.Progress = 1
		if res != nil {
			st.Created = res.Created
			st.Existing = res.Existing
			st.Skipped = res.Skipped
			st.Errors = res.Errors
			if len(res.Errors) > 50 {
				st.Errors = res.Errors[:50]
			}
		}
	})
}

func (s *Server) loadStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	st, err := s.runs.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) startTest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":          "test execution not implemented yet",
		"notImplemented": true,
		"message":        "Load a target first; test harness lands in a later iteration.",
	})
}

func (s *Server) getResults(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Prefer a completed load run; else treat id as plan id.
	for _, st := range s.runs.List() {
		if st.ID == id || st.PlanID == id || st.TargetID == id {
			if st.Kind == "load" {
				writeJSON(w, http.StatusOK, map[string]any{
					"id":      st.ID,
					"planId":  st.PlanID,
					"summary": st,
					"notes":   "Load-time metrics only. Latency/throughput test execution is not implemented yet.",
				})
				return
			}
		}
	}
	plan, err := compose.Load(s.runtime.Paths.PlansDir, id)
	if err != nil {
		// fall back: latest plan for target card id
		plans, _ := compose.List(s.runtime.Paths.PlansDir)
		if len(plans) == 0 {
			http.Error(w, "no results", http.StatusNotFound)
			return
		}
		plan = plans[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     plan.Metadata.ID,
		"planId": plan.Metadata.ID,
		"summary": map[string]any{
			"totalNamespaces":   plan.Summary.TotalNamespaces,
			"totalRecords":      plan.Summary.TotalRecords,
			"estimatedTotalGiB": plan.Summary.TotalSizeGiB,
			"totalSecrets":      plan.Target.ObjectTotals["secrets"],
			"totalConfigMaps":   plan.Target.ObjectTotals["configmaps"],
		},
		"notes": "Plan estimate only — cluster not necessarily loaded.",
		"plan":  plan,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// StaticFS serves an embedded SPA (interview-me style).
type StaticFS struct {
	Root http.FileSystem
}

func (s StaticFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.Root == nil {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	f, err := s.Root.Open(path)
	if err != nil || isDir(f) {
		if f != nil {
			_ = f.Close()
		}
		f, err = s.Root.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path = "index.html"
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "static file not seekable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), rs)
}

func isDir(f http.File) bool {
	if f == nil {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.IsDir()
}

// ListenAndServe starts the HTTP server (used by CLI serve).
func ListenAndServe(addr string, h http.Handler) error {
	log.Printf("listening on %s (WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT)", addr)
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// EnsureDataDirs is a helper for serve/CLI.
func EnsureDataDirs(rt *config.RuntimeConfig) error {
	return config.EnsureDirs(rt)
}

// WriteRuntimeExample writes a starter runtime.yaml if missing.
func WriteRuntimeExample(path string, rt *config.RuntimeConfig) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	b, err := os.ReadFile("config/runtime.example.yaml")
	if err == nil {
		return os.WriteFile(path, b, 0o644)
	}
	// fallback: marshal current
	return nil
}
