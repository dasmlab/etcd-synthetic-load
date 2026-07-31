package loadgen

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/compose"
	"github.com/dasmlab/etcd-synthetic-load/internal/config"
	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

var (
	routeGVR  = schema.GroupVersionResource{Group: "route.openshift.io", Version: "v1", Resource: "routes"}
	egressGVR = schema.GroupVersionResource{Group: "k8s.ovn.org", Version: "v1", Resource: "egressfirewalls"}
)

// PacedOptions configures a controlled load from a compose.Plan.
type PacedOptions struct {
	Client     kubernetes.Interface
	Dynamic    dynamic.Interface
	Plan       *compose.Plan
	Runtime    *config.RuntimeConfig
	DryRun     bool
	Confirm    bool
	ProgressFn func(done, total int64, message string)
}

// PacedResult summarizes a paced load.
type PacedResult struct {
	DryRun            bool
	NamespacesCreated int
	Created           map[string]int
	Existing          map[string]int
	Skipped           map[string]int
	Errors            []string
}

type pacedJob struct {
	kind      compose.ObjectKind
	namespace string
	name      string
	tier      string
	sizeBytes int
	planID    string
}

// RunPaced applies a compose.Plan with batching/concurrency pauses.
func RunPaced(ctx context.Context, opts PacedOptions) (*PacedResult, error) {
	if opts.Plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	if err := EnsureNoDangerousDefaults(opts.Confirm, opts.DryRun); err != nil {
		return nil, err
	}

	rt := opts.Runtime
	if rt == nil {
		d := config.DefaultRuntime()
		rt = &d
	}
	batchSize := rt.Load.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	concurrency := rt.Load.Concurrency
	if concurrency <= 0 {
		concurrency = 8
	}
	batchPause := rt.Load.PauseBetweenBatches.Std()
	nsPause := rt.Load.NamespacePause.Std()

	res := &PacedResult{
		DryRun:   opts.DryRun,
		Created:  map[string]int{},
		Existing: map[string]int{},
		Skipped:  map[string]int{},
	}

	var jobs []pacedJob
	prefix := "esl"
	planID := opts.Plan.Metadata.ID

	for _, tier := range opts.Plan.Tiers {
		tierKey := sanitizeTier(tier.Name)
		for i := 0; i < tier.NamespaceCount; i++ {
			ns := NamespaceName(prefix, tierKey, i)
			for _, c := range tier.Composition {
				perNS := c.RecordCount / tier.NamespaceCount
				rem := c.RecordCount % tier.NamespaceCount
				n := perNS
				if i < rem {
					n++
				}
				for j := 0; j < n; j++ {
					sz := randomInRange(c.SizeRange.SmallX, c.SizeRange.LargeX)
					jobs = append(jobs, pacedJob{
						kind:      c.Kind,
						namespace: ns,
						name:      fmt.Sprintf("esl-%s-%06d", shortKind(c.Kind), j+1),
						tier:      tier.Name,
						sizeBytes: sz,
						planID:    planID,
					})
				}
			}
		}
	}

	total := int64(len(jobs) + sumNamespaceCounts(opts.Plan))
	if opts.DryRun {
		for _, j := range jobs {
			res.Created[string(j.kind)]++
		}
		res.NamespacesCreated = sumNamespaceCounts(opts.Plan)
		if opts.ProgressFn != nil {
			opts.ProgressFn(total, total, "dry-run complete")
		}
		return res, nil
	}
	if opts.Client == nil {
		return nil, fmt.Errorf("kubernetes client required for real load")
	}

	var done int64
	report := func(msg string) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(atomic.LoadInt64(&done), total, msg)
		}
	}

	// Create namespaces sequentially (controlled).
	seenNS := map[string]bool{}
	for _, j := range jobs {
		if seenNS[j.namespace] {
			continue
		}
		seenNS[j.namespace] = true
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		ns := BuildNamespace(j.namespace, j.tier, planID, nil)
		_, err := opts.Client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		switch {
		case err == nil:
			res.NamespacesCreated++
		case apierrors.IsAlreadyExists(err):
			// ok
		default:
			res.Errors = append(res.Errors, fmt.Sprintf("namespace %s: %v", j.namespace, err))
			continue
		}
		atomic.AddInt64(&done, 1)
		report(fmt.Sprintf("namespace %s ready", j.namespace))
		if nsPause > 0 {
			time.Sleep(nsPause)
		}
	}

	// Apply objects in paced batches.
	var mu sync.Mutex
	for start := 0; start < len(jobs); start += batchSize {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		end := start + batchSize
		if end > len(jobs) {
			end = len(jobs)
		}
		batch := jobs[start:end]
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, j := range batch {
			j := j
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				created, existing, skipped, err := createObject(ctx, opts, j)
				mu.Lock()
				if err != nil {
					res.Errors = append(res.Errors, err.Error())
				}
				if created {
					res.Created[string(j.kind)]++
				}
				if existing {
					res.Existing[string(j.kind)]++
				}
				if skipped {
					res.Skipped[string(j.kind)]++
				}
				mu.Unlock()
				atomic.AddInt64(&done, 1)
			}()
		}
		wg.Wait()
		report(fmt.Sprintf("batch %d-%d/%d", start+1, end, len(jobs)))
		if batchPause > 0 && end < len(jobs) {
			time.Sleep(batchPause)
		}
	}
	report("load complete")
	return res, nil
}

func createObject(ctx context.Context, opts PacedOptions, j pacedJob) (created, existing, skipped bool, err error) {
	labels := map[string]string{
		profile.LabelManaged:   "true",
		profile.LabelProfileID: j.planID,
		profile.LabelTier:      j.tier,
		profile.LabelKind:      profile.KindGeneric,
	}
	switch j.kind {
	case compose.KindSecrets:
		sec := BuildGenericSecret(j.namespace, j.name, j.tier, j.planID, j.sizeBytes)
		_, e := opts.Client.CoreV1().Secrets(j.namespace).Create(ctx, sec, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindConfigMaps:
		cm := BuildConfigMap(j.namespace, j.name, j.tier, j.planID, j.sizeBytes)
		_, e := opts.Client.CoreV1().ConfigMaps(j.namespace).Create(ctx, cm, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindServiceAccounts:
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: j.name, Namespace: j.namespace, Labels: labels, Annotations: padAnnotations(j.sizeBytes)},
		}
		_, e := opts.Client.CoreV1().ServiceAccounts(j.namespace).Create(ctx, sa, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindServices:
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: j.name, Namespace: j.namespace, Labels: labels, Annotations: padAnnotations(j.sizeBytes)},
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{{Port: 80, Name: "http"}},
				Selector: map[string]string{"app": j.name},
			},
		}
		_, e := opts.Client.CoreV1().Services(j.namespace).Create(ctx, svc, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindRoleBindings:
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: j.name, Namespace: j.namespace, Labels: labels, Annotations: padAnnotations(j.sizeBytes)},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "view"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: j.namespace}},
		}
		_, e := opts.Client.RbacV1().RoleBindings(j.namespace).Create(ctx, rb, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindRoutes:
		if opts.Dynamic == nil {
			return false, false, true, nil
		}
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "route.openshift.io/v1",
			"kind":       "Route",
			"metadata": map[string]interface{}{
				"name":        j.name,
				"namespace":   j.namespace,
				"labels":      labels,
				"annotations": padAnnotations(j.sizeBytes),
			},
			"spec": map[string]interface{}{
				"to": map[string]interface{}{"kind": "Service", "name": "does-not-need-to-exist"},
			},
		}}
		_, e := opts.Dynamic.Resource(routeGVR).Namespace(j.namespace).Create(ctx, obj, metav1.CreateOptions{})
		return classifyCreate(e)
	case compose.KindEgressFirewalls:
		if opts.Dynamic == nil {
			return false, false, true, nil
		}
		// One EgressFirewall per namespace typically — skip extras.
		name := "default"
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "k8s.ovn.org/v1",
			"kind":       "EgressFirewall",
			"metadata": map[string]interface{}{
				"name":        name,
				"namespace":   j.namespace,
				"labels":      labels,
				"annotations": padAnnotations(j.sizeBytes),
			},
			"spec": map[string]interface{}{
				"egress": []interface{}{
					map[string]interface{}{
						"type": "Allow",
						"to":   map[string]interface{}{"cidrSelector": "0.0.0.0/0"},
					},
				},
			},
		}}
		_, e := opts.Dynamic.Resource(egressGVR).Namespace(j.namespace).Create(ctx, obj, metav1.CreateOptions{})
		return classifyCreate(e)
	default:
		return false, false, true, nil
	}
}

func classifyCreate(err error) (created, existing, skipped bool, outErr error) {
	if err == nil {
		return true, false, false, nil
	}
	if apierrors.IsAlreadyExists(err) {
		return false, true, false, nil
	}
	// Missing CRD / no permission → skip rather than hard-fail whole run.
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
		return false, false, true, nil
	}
	return false, false, false, err
}

func padAnnotations(size int) map[string]string {
	if size <= 0 {
		return nil
	}
	// Cap annotation pad so we don't blow API limits; secrets/cms carry real size.
	if size > 8000 {
		size = 8000
	}
	return map[string]string{
		"etcd-synthetic-load.dasmlab.org/pad": string(GeneratePayload(size, "ann")),
	}
}

func randomInRange(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + rand.Intn(hi-lo+1)
}

func sumNamespaceCounts(p *compose.Plan) int {
	n := 0
	for _, t := range p.Tiers {
		n += t.NamespaceCount
	}
	return n
}

func sanitizeTier(name string) string {
	switch name {
	case "SMALL":
		return "small"
	case "MEDIUM":
		return "medium"
	case "LARGE":
		return "large"
	default:
		return name
	}
}

func shortKind(k compose.ObjectKind) string {
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
