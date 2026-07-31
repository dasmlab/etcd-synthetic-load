package loadgen

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

// Options configure a load run.
type Options struct {
	Client      kubernetes.Interface
	Profile     *profile.Profile
	DryRun      bool
	Concurrency int
	// ProgressFn, if set, is called periodically with (objectsDone,
	// objectsTotal) while a real (non-dry-run) load is in progress.
	ProgressFn func(done, total int64)
}

// Result summarizes what happened (or, in dry-run, what would happen).
type Result struct {
	DryRun bool

	NamespacesCreated  int
	NamespacesExisting int
	SecretsCreated     int
	SecretsExisting    int
	ConfigMapsCreated  int
	ConfigMapsExisting int

	Errors []error
}

type job struct {
	kind         string // "configmap" | "secret" | "helmsecret"
	namespace    string
	name         string
	tier         string
	payloadBytes int
	revision     int
}

// Run applies (or, if DryRun, simulates applying) the given profile.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Profile == nil {
		return nil, fmt.Errorf("profile is required")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}

	res := &Result{DryRun: opts.DryRun}
	profileID := opts.Profile.Metadata.ID
	prefix := opts.Profile.Spec.NamespacePrefix

	var totalObjects int64
	for _, t := range opts.Profile.Spec.Tiers {
		totalObjects += int64(t.NamespaceCount) * int64(t.Computed.SecretsPerNamespace+t.Computed.ConfigMapsPerNamespace)
	}

	if opts.DryRun {
		for _, t := range opts.Profile.Spec.Tiers {
			res.NamespacesCreated += t.NamespaceCount
			res.SecretsCreated += t.Computed.SecretsPerNamespace * t.NamespaceCount
			res.ConfigMapsCreated += t.Computed.ConfigMapsPerNamespace * t.NamespaceCount
		}
		return res, nil
	}

	if opts.Client == nil {
		return nil, fmt.Errorf("client is required for a real (non-dry-run) load")
	}

	jobs := make(chan job, 1000)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var cmCreated, cmExisting, secCreated, secExisting int64
	var done int64

	for w := 0; w < opts.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				var err error
				switch j.kind {
				case "configmap":
					cm := BuildConfigMap(j.namespace, j.name, j.tier, profileID, j.payloadBytes)
					_, err = opts.Client.CoreV1().ConfigMaps(j.namespace).Create(ctx, cm, metav1.CreateOptions{})
					if err == nil {
						atomic.AddInt64(&cmCreated, 1)
					} else if apierrors.IsAlreadyExists(err) {
						atomic.AddInt64(&cmExisting, 1)
						err = nil
					}
				case "secret":
					sec := BuildGenericSecret(j.namespace, j.name, j.tier, profileID, j.payloadBytes)
					_, err = opts.Client.CoreV1().Secrets(j.namespace).Create(ctx, sec, metav1.CreateOptions{})
					if err == nil {
						atomic.AddInt64(&secCreated, 1)
					} else if apierrors.IsAlreadyExists(err) {
						atomic.AddInt64(&secExisting, 1)
						err = nil
					}
				case "helmsecret":
					sec := BuildHelmReleaseSecret(j.namespace, j.name, j.tier, profileID, j.payloadBytes, j.revision)
					_, err = opts.Client.CoreV1().Secrets(j.namespace).Create(ctx, sec, metav1.CreateOptions{})
					if err == nil {
						atomic.AddInt64(&secCreated, 1)
					} else if apierrors.IsAlreadyExists(err) {
						atomic.AddInt64(&secExisting, 1)
						err = nil
					}
				}
				atomic.AddInt64(&done, 1)
				if err != nil {
					errMu.Lock()
					res.Errors = append(res.Errors, fmt.Errorf("%s %s/%s: %w", j.kind, j.namespace, j.name, err))
					errMu.Unlock()
				}
			}
		}()
	}

	stopProgress := make(chan struct{})
	if opts.ProgressFn != nil {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					opts.ProgressFn(atomic.LoadInt64(&done), totalObjects)
				case <-stopProgress:
					return
				}
			}
		}()
	}

	// Producer: ensure each namespace exists (sequentially, namespaces are
	// cheap and low-cardinality), then enqueue its object jobs.
producerLoop:
	for _, t := range opts.Profile.Spec.Tiers {
		for i := 0; i < t.NamespaceCount; i++ {
			select {
			case <-ctx.Done():
				break producerLoop
			default:
			}

			nsName := NamespaceName(prefix, t.Name, i)
			ns := BuildNamespace(nsName, t.Name, profileID, opts.Profile.Spec.Labels)
			_, err := opts.Client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			switch {
			case err == nil:
				res.NamespacesCreated++
			case apierrors.IsAlreadyExists(err):
				res.NamespacesExisting++
			default:
				errMu.Lock()
				res.Errors = append(res.Errors, fmt.Errorf("namespace %s: %w", nsName, err))
				errMu.Unlock()
				continue
			}

			for c := 0; c < t.Computed.ConfigMapsPerNamespace; c++ {
				jobs <- job{
					kind:         "configmap",
					namespace:    nsName,
					name:         fmt.Sprintf("esl-cm-%06d", c+1),
					tier:         t.Name,
					payloadBytes: t.Computed.ConfigMapPayloadBytes,
				}
			}

			for s := 0; s < t.Computed.SecretsPerNamespace; s++ {
				name := fmt.Sprintf("esl-sec-%06d", s+1)
				if s < t.Computed.HelmSecretsPerNamespace {
					jobs <- job{
						kind:         "helmsecret",
						namespace:    nsName,
						name:         name + "-helm.v" + fmt.Sprintf("%d", (s%10)+1),
						tier:         t.Name,
						payloadBytes: t.Computed.HelmSecretPayloadBytes,
						revision:     (s % 10) + 1,
					}
				} else {
					jobs <- job{
						kind:         "secret",
						namespace:    nsName,
						name:         name,
						tier:         t.Name,
						payloadBytes: t.Computed.SecretPayloadBytes,
					}
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
	close(stopProgress)

	res.ConfigMapsCreated = int(cmCreated)
	res.ConfigMapsExisting = int(cmExisting)
	res.SecretsCreated = int(secCreated)
	res.SecretsExisting = int(secExisting)

	if opts.ProgressFn != nil {
		opts.ProgressFn(atomic.LoadInt64(&done), totalObjects)
	}

	return res, nil
}

// EnsureNoDangerousDefaults is a small guard used by cmd/ to make sure a
// real (non-dry-run) load can never be triggered without the explicit
// safety acknowledgement flag. Kept here so both the CLI and any future
// callers share one source of truth.
func EnsureNoDangerousDefaults(understandFlag bool, dryRun bool) error {
	if dryRun {
		return nil
	}
	if !understandFlag {
		return fmt.Errorf("refusing to run a real (non-dry-run) load without --i-understand-this-stresses-etcd")
	}
	return nil
}
