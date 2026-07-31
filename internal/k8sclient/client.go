// Package k8sclient builds a Kubernetes clientset from, in order of
// preference:
//
//  1. An explicit --kubeconfig flag path.
//  2. The KUBECONFIG environment variable / mounted kubeconfig.
//  3. OC_SERVER + OC_USER + OC_PASSWORD env vars: shells out to `oc login`
//     to mint a short-lived kubeconfig in a temp dir, then loads that.
//  4. In-cluster config (when running as a Pod with a service account).
//  5. The default kubeconfig loading rules (~/.kube/config).
package k8sclient

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Options controls how the client is built.
type Options struct {
	KubeconfigPath string

	// OC_* login fallback, used when no kubeconfig is available.
	OCServer   string
	OCUser     string
	OCPassword string
	// OCInsecureSkipTLSVerify allows connecting to clusters with
	// self-signed/lab certs via `oc login --insecure-skip-tls-verify`.
	OCInsecureSkipTLSVerify bool
}

// OptionsFromEnv reads the OC_SERVER / OC_USER / OC_PASSWORD /
// OC_INSECURE_SKIP_TLS_VERIFY / KUBECONFIG environment variables.
func OptionsFromEnv(kubeconfigFlag string) Options {
	return Options{
		KubeconfigPath:          kubeconfigFlag,
		OCServer:                os.Getenv("OC_SERVER"),
		OCUser:                  os.Getenv("OC_USER"),
		OCPassword:              os.Getenv("OC_PASSWORD"),
		OCInsecureSkipTLSVerify: os.Getenv("OC_INSECURE_SKIP_TLS_VERIFY") == "true",
	}
}

// BuildConfig resolves a *rest.Config using the precedence documented on
// the package.
func BuildConfig(opts Options) (*rest.Config, error) {
	if opts.KubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", opts.KubeconfigPath)
	}

	if kc := os.Getenv("KUBECONFIG"); kc != "" {
		return clientcmd.BuildConfigFromFlags("", kc)
	}

	if opts.OCServer != "" && opts.OCUser != "" && opts.OCPassword != "" {
		return buildConfigFromOCLogin(opts)
	}

	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	if home, err := os.UserHomeDir(); err == nil {
		def := filepath.Join(home, ".kube", "config")
		if _, statErr := os.Stat(def); statErr == nil {
			return clientcmd.BuildConfigFromFlags("", def)
		}
	}

	return nil, fmt.Errorf("no kubeconfig found: pass --kubeconfig, set KUBECONFIG, set OC_SERVER/OC_USER/OC_PASSWORD, " +
		"or run in-cluster")
}

// buildConfigFromOCLogin shells out to the `oc` CLI (must be present on
// PATH, e.g. baked into the container image) to authenticate and produce a
// throwaway kubeconfig, which is then loaded normally via client-go. This
// lets the tool support username/password OpenShift auth (including
// OAuth-backed identity providers that `oc login` handles for us) without
// reimplementing OpenShift's login flow in client-go directly.
func buildConfigFromOCLogin(opts Options) (*rest.Config, error) {
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return nil, fmt.Errorf("OC_SERVER/OC_USER/OC_PASSWORD set but `oc` binary not found on PATH: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "etcd-synthetic-load-kubeconfig-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir for oc login kubeconfig: %w", err)
	}
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	args := []string{
		"login",
		opts.OCServer,
		"-u", opts.OCUser,
		"-p", opts.OCPassword,
	}
	if opts.OCInsecureSkipTLSVerify {
		args = append(args, "--insecure-skip-tls-verify=true")
	}

	cmd := exec.Command(ocPath, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfigPath)
	// Deliberately do not log stdout/stderr verbatim since `oc login`
	// echoes back the server/user (not the password) - still, keep output
	// only for error diagnostics, never on success.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("oc login failed: %w (output: %s)", err, string(out))
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

// New builds a clientset using BuildConfig.
func New(opts Options) (kubernetes.Interface, error) {
	cfg, err := BuildConfig(opts)
	if err != nil {
		return nil, err
	}
	// The load command intentionally creates huge numbers of objects in a
	// short window; raise client-side QPS/burst above the (low) client-go
	// defaults so we're not artificially throttled before the server's own
	// limits kick in.
	cfg.QPS = 50
	cfg.Burst = 100
	return kubernetes.NewForConfig(cfg)
}
