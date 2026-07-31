// Command etcd-synthetic-load is a synthetic load generator that creates
// large volumes of Secrets and ConfigMaps on an OpenShift/Kubernetes
// cluster in order to exercise etcd, so that triage tooling can be tested
// against a realistic (but disposable) high-object-count profile.
//
// ============================================================================
// WARNING: THIS TOOL IS NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.
// It intentionally stresses etcd. Lab/Test/Dev clusters ONLY.
// ============================================================================
package main

import (
	"fmt"
	"os"
)

const banner = `
================================================================================
  WARNING: etcd-synthetic-load intentionally STRESSES ETCD.
  It creates large volumes of Secrets, ConfigMaps, and Namespaces.

  DO NOT run 'load' against any cluster that is important to you or anyone
  else. LAB / TEST / DEV CLUSTERS ONLY. There is no undo for a cluster that
  falls over from etcd exhaustion.

  Running 'load' for real (non-dry-run) requires the explicit flag:
      --i-understand-this-stresses-etcd
================================================================================
`

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
