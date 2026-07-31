// Package status collects a live view of what this tool has created on a
// cluster, keyed off the etcd-synthetic-load.dasmlab.org/managed label.
package status

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

// NamespaceStat is the per-namespace rollup.
type NamespaceStat struct {
	Name        string
	Tier        string
	ProfileID   string
	Secrets     int
	HelmSecrets int
	ConfigMaps  int
	ApproxBytes int64
}

// Result is the full cluster-wide rollup returned by Collect.
type Result struct {
	Namespaces []NamespaceStat

	TotalNamespaces  int
	TotalSecrets     int
	TotalHelmSecrets int
	TotalConfigMaps  int
	TotalApproxBytes int64
}

// TotalApproxGiB is a convenience accessor.
func (r *Result) TotalApproxGiB() float64 {
	return float64(r.TotalApproxBytes) / (1024 * 1024 * 1024)
}

const pageSize = int64(500)

// Collect scans the cluster for everything labeled as belonging to this
// tool (optionally scoped to a single profileID) and returns a rollup.
//
// Note: computing ApproxBytes requires fetching full Secret/ConfigMap
// objects (including Data), so this call transfers real payload volume
// over the network - for a 5+ GiB harness this can take a while and use
// meaningful bandwidth. That's an inherent trade-off of measuring the
// thing the tool is designed to create.
func Collect(ctx context.Context, client kubernetes.Interface, profileID string) (*Result, error) {
	selector := profile.LabelManaged + "=true"
	if profileID != "" {
		selector = fmt.Sprintf("%s,%s=%s", selector, profile.LabelProfileID, profileID)
	}

	stats := map[string]*NamespaceStat{}
	getOrCreate := func(name string) *NamespaceStat {
		st, ok := stats[name]
		if !ok {
			st = &NamespaceStat{Name: name}
			stats[name] = st
		}
		return st
	}

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	for _, ns := range nsList.Items {
		st := getOrCreate(ns.Name)
		st.Tier = ns.Labels[profile.LabelTier]
		st.ProfileID = ns.Labels[profile.LabelProfileID]
	}

	cont := ""
	for {
		list, err := client.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: pageSize, Continue: cont})
		if err != nil {
			return nil, fmt.Errorf("list configmaps: %w", err)
		}
		for _, cm := range list.Items {
			st := getOrCreate(cm.Namespace)
			if st.Tier == "" {
				st.Tier = cm.Labels[profile.LabelTier]
			}
			if st.ProfileID == "" {
				st.ProfileID = cm.Labels[profile.LabelProfileID]
			}
			st.ConfigMaps++
			for _, v := range cm.Data {
				st.ApproxBytes += int64(len(v))
			}
			for _, v := range cm.BinaryData {
				st.ApproxBytes += int64(len(v))
			}
		}
		cont = list.Continue
		if cont == "" {
			break
		}
	}

	cont = ""
	for {
		list, err := client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: pageSize, Continue: cont})
		if err != nil {
			return nil, fmt.Errorf("list secrets: %w", err)
		}
		for _, sec := range list.Items {
			st := getOrCreate(sec.Namespace)
			if st.Tier == "" {
				st.Tier = sec.Labels[profile.LabelTier]
			}
			if st.ProfileID == "" {
				st.ProfileID = sec.Labels[profile.LabelProfileID]
			}
			st.Secrets++
			if sec.Labels[profile.LabelKind] == profile.KindHelmSecret {
				st.HelmSecrets++
			}
			for _, v := range sec.Data {
				st.ApproxBytes += int64(len(v))
			}
		}
		cont = list.Continue
		if cont == "" {
			break
		}
	}

	res := &Result{}
	for _, st := range stats {
		res.Namespaces = append(res.Namespaces, *st)
		res.TotalSecrets += st.Secrets
		res.TotalHelmSecrets += st.HelmSecrets
		res.TotalConfigMaps += st.ConfigMaps
		res.TotalApproxBytes += st.ApproxBytes
	}
	res.TotalNamespaces = len(res.Namespaces)
	sort.Slice(res.Namespaces, func(i, j int) bool { return res.Namespaces[i].Name < res.Namespaces[j].Name })

	return res, nil
}
