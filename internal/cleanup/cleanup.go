// Package cleanup deletes everything this tool created, identified purely
// by the etcd-synthetic-load.dasmlab.org/managed label (and, optionally, a
// specific profile-id), so cleanup is safe even if profile.yaml has been
// lost or edited.
package cleanup

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

// Options configures a cleanup run.
type Options struct {
	Client      kubernetes.Interface
	ProfileID   string // optional; empty means "all etcd-synthetic-load namespaces"
	DryRun      bool
	Wait        bool
	WaitTimeout time.Duration
}

// Result summarizes what was (or would be) deleted.
type Result struct {
	DryRun              bool
	NamespacesDeleted   []string
	NamespacesRemaining []string
}

// Run deletes (or, if DryRun, lists) every managed namespace. Deleting a
// namespace cascades to all Secrets/ConfigMaps/etc inside it, so we don't
// need to delete objects individually.
func Run(ctx context.Context, opts Options) (*Result, error) {
	selector := profile.LabelManaged + "=true"
	if opts.ProfileID != "" {
		selector = fmt.Sprintf("%s,%s=%s", selector, profile.LabelProfileID, opts.ProfileID)
	}

	nsList, err := opts.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	res := &Result{DryRun: opts.DryRun}
	for _, ns := range nsList.Items {
		res.NamespacesDeleted = append(res.NamespacesDeleted, ns.Name)
	}

	if opts.DryRun {
		return res, nil
	}

	for _, ns := range nsList.Items {
		if err := opts.Client.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{}); err != nil {
			return res, fmt.Errorf("delete namespace %s: %w", ns.Name, err)
		}
	}

	if !opts.Wait {
		return res, nil
	}

	timeout := opts.WaitTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining, err := opts.Client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return res, fmt.Errorf("poll namespaces during wait: %w", err)
		}
		if len(remaining.Items) == 0 {
			return res, nil
		}
		res.NamespacesRemaining = nil
		for _, ns := range remaining.Items {
			res.NamespacesRemaining = append(res.NamespacesRemaining, ns.Name)
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	return res, fmt.Errorf("timed out after %s waiting for %d namespace(s) to terminate", timeout, len(res.NamespacesRemaining))
}
