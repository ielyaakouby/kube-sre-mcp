# Diagnostic Engine

The Diagnostic Engine (`internal/diagnostic`) answers SRE questions that a raw
object dump does not:

* What is unhealthy?
* Why is it unhealthy?
* What Evidence supports the diagnosis?
* What is impacted?
* What should be done next?

It is deterministic. The same Kubernetes state produces the same structured
response. The MCP client may narrate the JSON; it does not change the ranking.

Primary tool: `k8s_diagnose_resource`. Kind-specific wrappers:
`k8s_diagnose_pod`, `k8s_diagnose_deployment`, `k8s_diagnose_service`,
`k8s_diagnose_node`. Other kinds (StatefulSet, DaemonSet, Job, CronJob, Ingress,
PVC, and generic CRDs) go through `k8s_diagnose_resource`.

## Purpose

Diagnosis is not “return `kubectl get` YAML”. The engine:

1. Resolves the target (kind aliases, optional prefix, no silent guess).
2. Collects independent evidence sources.
3. Normalizes them as Signals.
4. Correlates Signals into ranked Root Cause Hypotheses.
5. Reports Health, Impact, Visibility, and Recommendations.

If evidence is incomplete, Health is `unknown` (or status `partial`), not
`healthy`.

## Diagnostic Pipeline

```text
Resolve Target
      ↓
Build Context / Graph
      ↓
Collect Status / Conditions
      ↓
Collect Events
      ↓
Collect Logs
      ↓
Inspect Dependencies
      ↓
Optional Metrics
      ↓
Normalize Signals
      ↓
Correlate
      ↓
Rank Root Causes
      ↓
Compute Impact
      ↓
Generate Recommendations
```

Resolution lives in `internal/kube`. Graph construction lives in
`internal/graph` ([resource-graph.md](resource-graph.md)). Correlation lives
in `internal/correlation`. Recommendations live in `internal/recommendation`.
Assembly (`assemble`) produces the `DiagnosticResponse`.

Prefix resolution (`resolution: prefix`) demotes `confirmed` confidence to
`high` so a prefix match cannot claim authoritative certainty.

## Capability Matrix

| Resource | Health | Events | Logs | Dependencies | RCA | Depth |
| --- | --- | --- | --- | --- | --- | --- |
| Pod | Deep | Yes | Yes (failing container; previous on restart) | Yes (CM/Secret/PVC/SA/imagePullSecrets/Node) | Deep | `full` |
| Deployment | Deep (replica + child) | Yes (+ current ReplicaSets) | Child Pods (capped) | Yes (template + HPA/PDB links) | Aggregated | `full` |
| StatefulSet | Deep | Yes | Child Pods (capped) | Yes (Pods, PVC, PV, StorageClass, HPA) | Aggregated | `full` |
| DaemonSet | Deep | Yes | Child Pods (capped) | Yes | Aggregated | `full` |
| Job | Deep | Yes | Child Pods (capped; includes healthy-looking failures) | Yes | Aggregated | `full` |
| CronJob | Deep | Yes | Via owned Jobs (capped) | Yes | Aggregated | `full` |
| Service | Deep | Yes | Child Pods when selected | Yes (selector, EndpointSlice, Endpoints) | Deep | `full` |
| Ingress | Deep | Yes | No (backend Services/slices) | Yes (Service, TLS Secret metadata, IngressClass) | Deep | `full` |
| PersistentVolumeClaim | Deep | Yes | No | Partial (PV, StorageClass) | Deep | `full` |
| Node | Deep | Yes | No | Partial (hosted Pod count) | Deep | `full` |
| Other / CRD | Generic conditions | No dedicated analyzer | No | No graph walk | Generic | `generic` |

**Supported** means a kind-specific analyzer exists.
**Partial** means some related objects are inspected, not a full graph of every
possible peer.
**Generic** means `status.conditions` only; Health stays `unknown` when no
False/Degraded conditions are found.
**Unsupported** as a first-class diagnostic kind: arbitrary `kubectl exec`,
live `kubectl apply`, and domain-specific CRD controllers.

`k8s_get_resource` / `k8s_list_resources` / `k8s_find_resource` can still
inspect kinds the Diagnostic Engine does not specialize (ConfigMap, Secret
metadata, Namespace, HPA, PDB, NetworkPolicy, …) via discovery/dynamic client.

## Supported Resource Diagnostics

Kind-specific analyzers in `Engine.diagnose`:

* Pod
* Deployment
* StatefulSet
* DaemonSet
* Job
* CronJob
* Service
* Ingress
* PersistentVolumeClaim
* Node
* **generic CRDs / unknown kinds** where discovery can map the kind — `diagnostic_depth: generic`

Kind aliases accepted by the resolver include `po`, `pod`, `deploy`, `sts`,
`ds`, `svc`, `ing`, `pvc`, `job`, `cronjob`, `node`, and others listed in
`internal/kube/discovery.go`. Unknown strings are passed through to RESTMapper.

## Pod Diagnosis

`DiagnosePod` inspects:

| Category | Source | Notes |
| --- | --- | --- |
| ImagePullBackOff / ErrImagePull | STATUS waiting reason | Correlation may refine to registry auth, not-found, rate-limit, TLS |
| CrashLoopBackOff | STATUS waiting / restarts | Logs (current + previous) are supporting Evidence |
| OOMKilled | lastState / terminated | Metrics high-memory is **not** treated as proof of a future OOM |
| Scheduling failures | SCHEDULER approximation + Events | Pending unscheduled Pods only |
| Missing dependencies | DEPENDENCY | Missing ConfigMap/Secret/PVC/ServiceAccount; unknown on 403 |
| Probes | STATUS / CONDITION | Readiness when running-not-ready; liveness/startup from condition hints |
| Storage | STATUS + Events + PVC GET | FailedMount / FailedAttachVolume |
| Node conditions | assigned Node | DiskPressure, MemoryPressure, NetworkUnavailable, cordon |
| Logs | LOG patterns | Confidence `low`; never authoritative alone |

Workload diagnosis (Deployment, StatefulSet, DaemonSet, Job, CronJob) aggregates
**child Pod** failures. Deployments prefer Pods owned by the **current**
ReplicaSet (older ReplicaSets are excluded from current RCA and noted in
Evidence). Child deep-diagnosis is capped by `KUBE_SRE_MCP_MAX_DEEP_PODS`
(default 8) and run with bounded concurrency.

## Workload Diagnosis

For controllers:

1. Read controller status (desired/ready/available, conditions such as
   `ProgressDeadlineExceeded`, Job `BackoffLimitExceeded`).
2. List child Pods by selector **and** confirm ownership via `ownerReferences` UID
   where the graph helpers do so (Deployment current ReplicaSet UID, STS/DS UID,
   CronJob→Job UID). Jobs currently list `job-name=<name>` (label), which is
   approximate relative to UID ownership.
3. Deep-diagnose up to `MaxDeepPods` unhealthy Pods (Jobs include failed Pods
   even when not filtered the same way).
4. Majority-reason aggregation becomes a workload-level Signal.
5. Impact uses replica counts (unavailable / desired / percent).

## Evidence Model

Signals (`internal/signal.DiagnosticSignal`) carry the full diagnostic fact:

| Field | Meaning |
| --- | --- |
| `id` | Stable-enough identifier, later scoped as `Kind/namespace/name:E-…` |
| `source` | `STATUS`, `CONDITION`, `EVENT`, `LOG`, `METRIC`, `DEPENDENCY`, `SCHEDULER`, `STORAGE`, `NETWORK`, `SECURITY` |
| `reason` | Machine category (e.g. `ImagePullBackOff`, `OOMKilled`, `INSUFFICIENT_CPU`) |
| `resource` | ResourceRef (apiVersion, kind, namespace, name, uid) |
| `severity` | `info` / `warning` / `critical` |
| `confidence` | `low` / `medium` / `high` / `confirmed` |

The MCP response `evidence` array is the Signal converted to `model.Evidence`
(`id`, `source`, `message`, `resource`, `timestamp`). Severity and confidence
remain on the Signal used for correlation; findings expose severity + reason +
message.

Source quality (used when ranking): STATUS/CONDITION > EVENT/LOG/DEPENDENCY >
SCHEDULER/STORAGE/NETWORK/METRIC.

## Root Cause Hypotheses

The Correlation Engine:

* deduplicates Event Signals by source/reason/message/resource (counts accumulate);
* classifies each Signal into a category (`IMAGE_PULL_FAILURE`, `CRASH_LOOP`, …);
* drops `healthy` / `noise`;
* builds one hypothesis per category with supporting Evidence IDs;
* may attach **contradicting** Evidence (e.g. CRASH_LOOP vs “appears healthy”);
* sorts deterministically: confidence, then weighted score, then category name, then title.

Multiple hypotheses are returned in `root_causes`. `root_cause` is the top
hypothesis, and is cleared when Health is `healthy`.

Weighted score boosts categories such as `OOM_KILLED`, `MISSING_SECRET`,
`REGISTRY_AUTHENTICATION_FAILURE`, and scheduling mismatches so they outrank a
generic `CRASH_LOOP` when both are present.

This is **not** first-match-wins.

## Confidence

| Value | Typical origin |
| --- | --- |
| `low` | Logs only; missing node list for scheduling; generic CRD conditions |
| `medium` | Default when a category has evidence but not independent STATUS+EVENT |
| `high` | Independent STATUS/CONDITION **and** EVENT; or authoritative status without the confirmed shortcut |
| `confirmed` | Direct Kubernetes status/condition that is treated as authoritative for that category (OOMKilled, ImagePullBackOff, CrashLoopBackOff, PVC pending, Node Ready=False, probe reasons) |

Explicit rules implemented today:

* **Logs alone cannot create authoritative certainty.** Log-only buckets stay `low`.
* Direct Kubernetes **status/conditions** may carry `confirmed` for selected categories.
* Contradicting Evidence demotes `confirmed` → `high` and `high` → `medium`.
* Prefix name resolution demotes `confirmed` → `high`.

Metrics Signals are `low` confidence supporting evidence.

## Impact

`model.Impact` fields used by analyzers:

| Field | Used by |
| --- | --- |
| `severity` | Mapped from Health (`critical`/`high`/`low`) |
| `unavailable_replicas` / `desired_replicas` / `percent_impacted` | Deployments and similar replica workloads |
| `ready_endpoints` / `total_endpoints` | Services (EndpointSlice / Endpoints) |
| `affected_nodes` / `workloads_on_node` | Nodes (hosted Pod count) |
| `summary` | Short human string |

Impact is attached to the response and copied onto the top hypothesis when the
workload analyzer sets replica impact.

## Partial Visibility

API failures are classified (`internal/kube`): `FORBIDDEN`, `UNAUTHORIZED`,
`TIMEOUT`, `CONNECTION_ERROR`, `API_ERROR`, `CANCELED`, `NOT_FOUND`.

`kube.Record` marks Visibility `limited` and records `failed_checks` /
`missing_permissions`. Health evaluation:

* required missing permissions or failed checks **and** no critical Signals **and** empty Signals → `unknown`;
* limited Visibility with no Signals → `unknown`;
* critical Signals still surface as `critical` even if Visibility is incomplete.

MCP status becomes `partial` when Visibility is limited after a successful get.
Permission denied on the **target itself** is `status: error`, Health `unknown`,
category `permission_denied`.

The system must not report `healthy` merely because it could not see Events,
logs, or child Pods.

## Scheduler Analysis

Pending Pods with **no** `spec.nodeName` get an **approximate eligibility**
check against listed Nodes (`schedule_mode: approximate_eligibility`):

Supported checks:

* Node Ready;
* cordon (`spec.unschedulable`);
* `nodeSelector`;
* required `nodeAffinity` `In` / `NotIn` / `Exists` / `DoesNotExist`;
* taints vs tolerations;
* Pod CPU/memory **requests vs node allocatable** (not vs remaining free after other Pods).

Unsupported (emitted as `UNSUPPORTED_SCHEDULER_CONSTRAINT`, reduced certainty):

* required **pod anti-affinity**;
* **topologySpreadConstraints**;
* nodeAffinity `Gt` / `Lt` operators.

If the node list cannot be read, mode is `events_only` — Events may still
explain FailedScheduling, but eligibility is not computed.

This is **not** a kube-scheduler simulation. It does not model preemption,
priority, volume topology beyond simple PVC signals, inter-pod affinity, or
runtime resource reservation.

## Metrics

When the metrics client is available (`metrics.k8s.io`):

* Pod diagnosis may flag high memory versus container limit (`high_memory`);
* Node diagnosis records whether node metrics were readable;
* `metrics_available` is set on the diagnostic response.

Missing metrics-server → `metrics_available: false` (or omitted). Diagnosis
continues.

Metrics are **supporting evidence only**. kube-sre-mcp does **not** perform
predictive memory-leak or future-OOM analysis. High current usage is explicitly
worded as “not proof of a leak or future OOM”.

## Cluster Health

`k8s_cluster_health` (`internal/health.Cluster`) is a separate, lighter path:

1. Inventory nodes, pods, deployments, statefulsets, daemonsets, jobs, PVCs.
2. Classify each with the same Health contract (replica counts / pod status / node conditions).
3. Optionally deep-diagnose a capped number of unhealthy candidates
   (`KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES`, default 8).
4. RBAC gaps on an inventory list make that part `unknown`, not healthy.

It does **not** run full RCA on every unhealthy object.

## Recommendations

`internal/recommendation.For` maps RCA category to ordered actions. Examples:

* registry authentication → verify imagePullSecret metadata (never Secret `.data`);
* OOM → compare usage vs limit; restart alone will not fix a leak;
* probes → fix the endpoint or timing; do not restart blindly;
* Service with no ready Pods → `k8s_diagnose_pod` on selected Pods.

`RestartUseful` returns false for image/auth/config/mount/unschedulable
categories. The restart Safe Action uses that policy unless `force=true`.
