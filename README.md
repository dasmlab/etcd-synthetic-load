# etcd-synthetic-load

> ================================================================================
>
> ## ⚠️ WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT ⚠️
>
> This tool **intentionally stresses etcd**. It creates large volumes of
> `Secret`, `ConfigMap`, and `Namespace` objects — by default targeting
> **~120,000 Secrets**, **~80,000 ConfigMaps**, and **~5.6 GiB** of estimated
> payload, out of an 8 GiB etcd quota.
>
> **LAB / TEST / DEV CLUSTERS ONLY.** Never point this at a cluster that
> anyone depends on. Etcd exhaustion can make a cluster's control plane
> unresponsive or unrecoverable. There is no "undo" button.
>
> Every real (non-dry-run) `load` invocation requires the explicit flag
> `--i-understand-this-stresses-etcd`. There is no default that lets you
> skip this.
>
> ================================================================================

## What this is

`etcd-synthetic-load` simulates a "client-like" etcd usage profile so that
etcd triage/monitoring tooling (e.g. dashboards, alerting, `etcdctl`-based
health scripts, capacity-planning scripts) can be exercised against
realistic object counts and payload sizes — without waiting for months of
organic workload growth, and without touching a real environment.

It generates a tunable **Small / Medium / Large namespace profile**:

- **Small** namespaces: many of them, low object density, small payloads.
- **Medium** namespaces: fewer of them, moderate object density and payloads.
- **Large** namespaces: very few, but dense and/or large-payload — modeling
  the "noisy neighbor" workloads (CI systems, GitOps controllers, service
  mesh, logging/observability agents) that in practice dominate real etcd
  usage.

It also generates **Helm-release-style secrets** (`type:
helm.sh/release.v1`, `owner=helm` labels) as a distinct, configurable impact
category, since orphaned Helm release history is a common, easily
overlooked source of etcd bloat in real clusters.

The Small/Medium/Large split, per-tier object counts, payload sizes, and
Helm-secret fraction are all tunable in `profile.yaml` (or via `plan` CLI
flags), specifically so you can sit down with a client/stakeholder and
agree the mix is representative of *their* cluster before running `load`.

## Reference client scale (from triage feedback)

Use these as knobs when agreeing a representative profile with the client —
defaults above are a dense 100-namespace shape; real migration clusters may
look more like:

| Signal | Example client observation |
|--------|----------------------------|
| Namespaces | ~**4,000** (not 100) |
| Secrets | ~**120,000** |
| ConfigMaps | ~**80,000** |
| etcd physical utilization | ~**5.6 GiB of 8 GiB** quota |
| Object-count metrics | Watch for **~2× inflation** if a tool `sum()`s `apiserver_storage_objects` across API-server replicas — correct aggregation is **`max by (resource)`**. Sanity-check against `oc get <resource> -A \| wc -l`. |

Example plan closer to a wide namespace sprawl (tune further with the client):

```bash
./bin/etcd-synthetic-load plan \
  --target-gib 5.6 \
  --target-secrets 120000 \
  --target-configmaps 80000 \
  --small-namespaces 3200 \
  --medium-namespaces 700 \
  --large-namespaces 100 \
  --helm-secret-fraction 0.15 \
  -o profile-4000ns.yaml
```

After load, validate triage scripts report namespace counts near `oc get ns | wc -l`
(≈1.0×), not ~2.0×.

## Non-goals

- This is **not** a general Kubernetes load-testing tool, benchmark suite,
  or chaos-engineering tool.
- It does not create Pods, Deployments, or any object that would actually
  run a workload — only etcd-resident metadata objects (Namespaces,
  Secrets, ConfigMaps).
- `EstimatedPayloadGiB` / `EstimatedTotalGiB` in `profile.yaml` are
  **estimates**, not a guarantee of resulting etcd DB size. Kubernetes and
  etcd add real overhead per object (object metadata, `managedFields`,
  `resourceVersion` history until compaction, boltdb page overhead, raft
  log/WAL growth) that this tool does not attempt to model precisely.
  **Always verify actual etcd size on your cluster** (e.g. `oc get --raw
  /metrics | grep etcd_mvcc_db_total_size_in_bytes`, or `etcdctl endpoint
  status -w table`) rather than trusting the estimate alone.

## Install / build

Requires Go 1.24+.

```bash
make build              # -> ./bin/etcd-synthetic-load
```

Or build a container image (requires network access to pull the UBI base
images and the OpenShift client binary):

```bash
make image               # IMAGE_TOOL=podman by default; override with IMAGE_TOOL=docker
make image IMAGE_NAME=quay.io/you/etcd-synthetic-load IMAGE_TAG=v0.1.0
```

## Cluster authentication

The tool builds a Kubernetes client using client-go, resolved in this order:

1. `--kubeconfig <path>` flag
2. `KUBECONFIG` environment variable (e.g. a mounted kubeconfig Secret/file)
3. `OC_SERVER` + `OC_USER` + `OC_PASSWORD` environment variables — the tool
   shells out to the `oc` CLI (`oc login`) to mint a short-lived kubeconfig
   in a temp directory, then uses that. Set `OC_INSECURE_SKIP_TLS_VERIFY=true`
   for lab clusters with self-signed certs. (The container image bundles
   `oc` for this reason.)
4. In-cluster config (when run as a Pod with a bound ServiceAccount)
5. `~/.kube/config`

Passwords/tokens are **never logged**. Secret/ConfigMap payload **values**
are also never logged (only lengths/counts).

## Quick start

```bash
# 1. Generate a profile aimed at the reference target
#    (~5.6 GiB payload, ~120k Secrets, ~80k ConfigMaps)
./bin/etcd-synthetic-load plan \
  --target-gib 5.6 \
  --target-secrets 120000 \
  --target-configmaps 80000 \
  -o profile.yaml

# 2. Sanity check it (no cluster contact at all)
./bin/etcd-synthetic-load load --profile profile.yaml --dry-run

# 3. Review profile.yaml with your client/stakeholder - agree the S/M/L mix,
#    payload sizes, and Helm-secret fraction are representative. Tune with
#    plan flags (see below) and regenerate as needed.

# 4. Apply it for real, against a LAB/TEST/DEV cluster only
export KUBECONFIG=/path/to/lab-cluster-kubeconfig
./bin/etcd-synthetic-load load --profile profile.yaml --i-understand-this-stresses-etcd

# 5. Check what's actually on the cluster
./bin/etcd-synthetic-load status

# 6. Tear it all down when you're done
./bin/etcd-synthetic-load cleanup
```

Equivalent `make` targets: `make plan`, `make load-dry-run`, `make
load-real`, `make status`, `make cleanup` / `make cleanup-yes`.

## CLI reference

### `plan`

Computes a tunable Small/Medium/Large profile that sums toward the given
targets, and writes it to `profile.yaml`.

```
etcd-synthetic-load plan [flags]

  --target-gib float                    target estimated payload size, in GiB (default 5.6)
  --target-secrets int                  target total number of Secrets (default 120000)
  --target-configmaps int               target total number of ConfigMaps (default 80000)
  --helm-secret-fraction float          fraction (0-1) of secrets generated as helm-release-style (default 0.15)
  --namespace-prefix string             prefix for generated namespace names (default "esl")
  --per-object-overhead-bytes int       estimated non-payload bytes/object used only for the GiB estimate (default 350)

  --small-namespaces int                (default 60)
  --medium-namespaces int               (default 30)
  --large-namespaces int                (default 10)

  --small-secret-fraction float         (default 0.15)
  --medium-secret-fraction float        (default 0.35)
  --large-secret-fraction float         (default 0.50)

  --small-configmap-fraction float      (default 0.15)
  --medium-configmap-fraction float     (default 0.35)
  --large-configmap-fraction float      (default 0.50)

  --small-secret-payload-bytes int      relative pre-scale payload size (default 100)
  --medium-secret-payload-bytes int     (default 1500)
  --large-secret-payload-bytes int      (default 10000)

  --small-configmap-payload-bytes int   (default 100)
  --medium-configmap-payload-bytes int  (default 1500)
  --large-configmap-payload-bytes int   (default 10000)

  --helm-secret-payload-multiplier float  helm secret payload = tier base secret payload * this (default 4)

  -o, --output string                   output path (default "profile.yaml")
```

**How the math works:** object *counts* per tier are derived directly from
`--target-secrets`/`--target-configmaps` times each tier's fraction, divided
across that tier's namespace count (so counts hit the requested totals
exactly, modulo integer rounding). Payload *sizes* are scaled **uniformly
across all tiers** (preserving the Small < Medium < Large ratio you set)
so that the resulting estimated payload approaches `--target-gib`. This
means: tune the `*-fraction` and `*-namespaces` flags to change the *shape*
of the load; the payload-bytes flags only set the *relative ratio* between
tiers, since `plan` will rescale their absolute magnitude for you.

### `load`

Applies `profile.yaml` to the cluster: creates namespaces, then Secrets and
ConfigMaps inside them, using a worker pool (`--concurrency`, default 20).

```
etcd-synthetic-load load [flags]

  --profile string                         path to profile.yaml (default "profile.yaml")
  --dry-run                                 compute and print planned counts, no cluster contact
  --concurrency int                         concurrent object-creation workers (default 20)
  --i-understand-this-stresses-etcd         REQUIRED for any real (non-dry-run) run
  --kubeconfig string                       (see Cluster authentication above)
```

`load` is **idempotent**: objects that already exist are silently skipped
(not treated as an error), so an interrupted run can simply be re-run to
"top up" toward the profile's targets.

### `status`

Scans the cluster for everything labeled
`etcd-synthetic-load.dasmlab.org/managed=true` and reports per-namespace and
total counts, plus an approximate payload size (sum of Secret/ConfigMap
data lengths — **not** the real etcd DB size).

```
etcd-synthetic-load status [--profile-id <id>] [--kubeconfig ...]
```

### `cleanup`

Deletes every namespace labeled
`etcd-synthetic-load.dasmlab.org/managed=true` (optionally scoped to
`--profile-id`). Deleting the namespace cascades to every Secret/ConfigMap
inside it — no separate object-level deletion needed.

```
etcd-synthetic-load cleanup [--profile-id <id>] [--dry-run] [--yes] [--wait] [--kubeconfig ...]
```

## Labels used

Every namespace and object created by this tool carries:

| Label | Meaning |
|---|---|
| `etcd-synthetic-load.dasmlab.org/managed=true` | Marks this object as owned by the tool (used by `status`/`cleanup`) |
| `etcd-synthetic-load.dasmlab.org/profile-id=<id>` | Which `plan` run (profile) this object belongs to |
| `etcd-synthetic-load.dasmlab.org/tier=<small\|medium\|large>` | Which namespace tier this belongs to |
| `etcd-synthetic-load.dasmlab.org/kind=<generic\|helm-release>` | Impact category |

This is what makes `cleanup` safe: it only ever touches namespaces carrying
the `managed=true` label, never anything else on the cluster.

## Profile shape (S/M/L sketch)

The default profile (see [`examples/profile.example.yaml`](examples/profile.example.yaml))
distributes load like this:

| Tier | Namespaces | Secrets/ns | ConfigMaps/ns | Secret payload | ConfigMap payload | Helm secret payload |
|---|---|---|---|---|---|---|
| Small  | 60 | ~300  | ~200  | ~400 B  | ~400 B  | ~1.7 KB |
| Medium | 30 | ~1400 | ~934  | ~6.3 KB | ~6.3 KB | ~25 KB  |
| Large  | 10 | ~6000 | ~4000 | ~42 KB  | ~42 KB  | ~169 KB |

Totals: **100 namespaces, ~120,000 Secrets (18,000 Helm-release style),
~80,000 ConfigMaps, ~5.6 GiB estimated payload.**

Relative proportions (tunable): Small tier gets 15% of objects spread over
60 namespaces (low density); Medium tier gets 35% over 30 namespaces
(moderate density); Large tier gets 50% over just 10 namespaces (very high
density) — modeling how a small number of workloads typically dominate real
etcd usage. Regenerate with different `--small-*`/`--medium-*`/`--large-*`
flags to match a specific client's actual namespace/workload distribution.

## Repository layout

```
cmd/etcd-synthetic-load/   CLI entrypoint (cobra commands: plan, load, status, cleanup)
internal/profile/          profile.yaml schema + plan math
internal/k8sclient/        client-go client construction (kubeconfig / OC_* / in-cluster)
internal/loadgen/          object builders + concurrent apply engine
internal/status/           cluster scan / rollup
internal/cleanup/          labeled-namespace deletion
examples/                  example profile.yaml
Containerfile              container build (bundles `oc` for OC_* login fallback)
Makefile                   build / image / plan / load-dry-run / status / cleanup targets
```

## Safety checklist before running `load` for real

- [ ] Confirmed this is a **lab/test/dev** cluster, not shared with anyone
      who'd be impacted by an outage.
- [ ] Confirmed current etcd usage/quota on the target cluster (don't blow
      past 8 GiB, or whatever the real quota is, without headroom).
- [ ] Reviewed `profile.yaml` and agreed the S/M/L mix is representative
      with whoever asked for this test.
- [ ] Ran `load --dry-run` first.
- [ ] Have a `cleanup` plan ready (and know how to run it) before you start.
