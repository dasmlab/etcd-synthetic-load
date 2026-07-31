package loadgen

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dasmlab/etcd-synthetic-load/internal/profile"
)

// NamespaceName returns the deterministic namespace name for tier t, index
// idx (0-based), under the given prefix.
func NamespaceName(prefix, tierName string, idx int) string {
	return fmt.Sprintf("%s-%s-%04d", prefix, tierName, idx+1)
}

// BuildNamespace constructs the Namespace object for a tier member.
func BuildNamespace(name, tierName, profileID string, extraLabels map[string]string) *corev1.Namespace {
	labels := mergeLabels(extraLabels, map[string]string{
		profile.LabelManaged:   "true",
		profile.LabelProfileID: profileID,
		profile.LabelTier:      tierName,
	})
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// BuildConfigMap constructs a generic synthetic ConfigMap.
func BuildConfigMap(namespace, name, tierName, profileID string, payloadBytes int) *corev1.ConfigMap {
	labels := map[string]string{
		profile.LabelManaged:   "true",
		profile.LabelProfileID: profileID,
		profile.LabelTier:      tierName,
		profile.LabelKind:      profile.KindGeneric,
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			"payload": string(GeneratePayload(payloadBytes, namespace+"/"+name)),
		},
	}
}

// BuildGenericSecret constructs a generic Opaque synthetic Secret.
func BuildGenericSecret(namespace, name, tierName, profileID string, payloadBytes int) *corev1.Secret {
	labels := map[string]string{
		profile.LabelManaged:   "true",
		profile.LabelProfileID: profileID,
		profile.LabelTier:      tierName,
		profile.LabelKind:      profile.KindGeneric,
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"payload": GeneratePayload(payloadBytes, namespace+"/"+name),
		},
	}
}

// BuildHelmReleaseSecret constructs a secret shaped like a Helm v3 release
// secret (type helm.sh/release.v1, owner=helm labels) so triage tooling can
// exercise "orphaned helm release history" style etcd bloat scenarios. The
// "release" data key holds synthetic filler, NOT a real chart manifest.
func BuildHelmReleaseSecret(namespace, name, tierName, profileID string, payloadBytes, revision int) *corev1.Secret {
	labels := map[string]string{
		profile.LabelManaged:   "true",
		profile.LabelProfileID: profileID,
		profile.LabelTier:      tierName,
		profile.LabelKind:      profile.KindHelmSecret,
		"owner":                "helm",
		"name":                 releaseNameFromSecretName(name),
		"status":               "superseded",
		"version":              fmt.Sprintf("%d", revision),
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": GeneratePayload(payloadBytes, namespace+"/"+name),
		},
	}
}

func releaseNameFromSecretName(secretName string) string {
	return "rel-" + secretName
}

func mergeLabels(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
