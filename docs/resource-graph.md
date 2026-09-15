# Resource Graph

Kubernetes objects cannot be diagnosed in isolation. A Deployment that is
`Progressing=False` is often explained by a ReplicaSet’s Pods; a Service with
no endpoints is explained by Pod readiness; a Pending Pod may be waiting on a
PVC, a taint, or a missing ConfigMap.

kube-sre-mcp builds a bounded **Resource Graph** (`internal/graph`) during
diagnosis and exposes edges as `dependencies` on the diagnostic response.

The graph is **context for RCA**, not a full cluster topology database. It is
rebuilt per diagnostic call. There is no persistent graph store.

## Why a Graph

```text
Deployment
→ ReplicaSet
→ Pod
→ Node
```

```text
Pod
├── ConfigMap
├── Secret
├── PVC
├── ServiceAccount
└── imagePullSecrets
```

```text
Service
→ EndpointSlice
→ Pods
```

```text
Ingress
→ Service
→ EndpointSlice
→ Pods
```

```text
StatefulSet
→ Pods
→ PVC
→ PV
→ StorageClass
```

```text
CronJob
→ Job
→ Pod
```

These walks match the builders in `internal/graph/dependencies.go`. Ingress
currently links **Services** (`ROUTED_BY_INGRESS`); EndpointSlices and Pods are
inspected by the Ingress analyzer, not always as additional graph nodes.

## Node Identity

Every graph node is a `ResourceRef`:

| Field | Purpose |
| --- | --- |
| `apiVersion` | Group/version when known (`apps/v1`, `v1`, `discovery.k8s.io/v1`, …) |
| `kind` | Kubernetes kind |
| `namespace` | Empty for cluster-scoped objects (Node, PV, StorageClass, IngressClass) |
| `name` | Object name |
| `uid` | Kubernetes UID when the object was fetched |

UID is used for cycle detection (`visited`) when present. Objects without UID
are deduplicated by kind + namespace + name.

## Relationships

Relation types are the `RelType` constants in `internal/graph/graph.go`:

| Relation | Meaning | Typical from → to |
| --- | --- | --- |
| `OWNS` | Controller owns child via ownerReference UID | Deployment → ReplicaSet → Pod; CronJob → Job |
| `OWNED_BY` | Inverse of `OWNS` | Pod → ReplicaSet; ReplicaSet → Deployment |
| `SELECTS` | Service selector matched a Pod | Service → Pod |
| `SCHEDULED_ON` | `spec.nodeName` | Pod → Node |
| `USES_CONFIGMAP` | env, envFrom, volume, projected | Pod/workload template → ConfigMap |
| `USES_SECRET` | env, envFrom, volume, projected | Pod → Secret |
| `USES_PVC` | volume claim or STS volumeClaimTemplate | Pod/StatefulSet → PVC |
| `USES_SERVICE_ACCOUNT` | `spec.serviceAccountName` or projected token | Pod → ServiceAccount |
| `USES_IMAGE_PULL_SECRET` | `imagePullSecrets` | Pod → Secret |
| `BACKED_BY_PV` | PVC `spec.volumeName` | PVC → PersistentVolume |
| `USES_STORAGE_CLASS` | PVC `storageClassName` | PVC → StorageClass |
| `EXPOSED_BY_SERVICE` | Inverse of Service selection | Pod → Service |
| `HAS_ENDPOINT` | EndpointSlice for the Service | Service → EndpointSlice |
| `ROUTED_BY_INGRESS` | Ingress backend Service | Service → Ingress |
| `SCALED_BY_HPA` | HPA `scaleTargetRef` matches | Deployment/StatefulSet → HorizontalPodAutoscaler |
| `PROTECTED_BY_PDB` | PDB selector overlaps workload selector | Deployment → PodDisruptionBudget |

There is no `ROUTES_TO` enum. Ingress uses `ROUTED_BY_INGRESS`.
There is no generic `BACKED_BY`; storage uses `BACKED_BY_PV`.
There is no `PROTECTED_BY`; disruption budgets use `PROTECTED_BY_PDB`.

HPA/PDB links are **best-effort list** operations. A list error can fail the
graph build for that workload and is recorded as limited Visibility.

## Ownership vs Labels

Where possible, kube-sre-mcp prefers **ownerReferences UID** over label
equality:

* ReplicaSets belonging to a Deployment: `OwnedByUID(rs, deployment.UID)`.
* Pods under a ReplicaSet / StatefulSet / DaemonSet / Job (graph helper):
  list by selector, then keep objects whose owner UID matches.
* Deployment **diagnosis** further restricts Pods to those owned by the
  **current** ReplicaSet(s), so old ReplicaSets from a rollout are not mixed
  into current RCA.
* CronJob Jobs: `OwnedByUID(job, cronJob.UID)`.
* StatefulSet PVCs: owner UID, or name derived from `volumeClaimTemplates`.

Label selection is still required to *find* candidates (Services, PDBs, HPAs,
EndpointSlices). Labels alone can match unrelated objects; UID ownership
prevents diagnosing the wrong replica set during a rollout.

Job diagnosis currently lists Pods with `job-name=<job>` (label), which is
slightly weaker than the graph builder’s UID filter. Treat Job Pod membership
as **approximately** the Job’s Pods.

## Limits

Graph construction is capped (`graph.Limits`):

| Setting | Env | Default |
| --- | --- | --- |
| Max depth | `KUBE_SRE_MCP_MAX_GRAPH_DEPTH` | `6` |
| Max nodes | `KUBE_SRE_MCP_MAX_GRAPH_NODES` | `80` |

If depth is exceeded: `truncated: true`, `truncation_reason: max_depth`.
If node count is exceeded: `truncated: true`, `truncation_reason: max_nodes`.

Visited UIDs prevent unbounded cycles. Analysis is **per request**, not a
cluster-wide crawl.

Workload diagnosis separately caps **deep Pod RCA** with
`KUBE_SRE_MCP_MAX_DEEP_PODS` (default 8). That cap can also set `truncated`
on Deployments when more unhealthy Pods exist than were deep-diagnosed.

## Graph and RCA

The graph does not rank hypotheses by itself. It:

* supplies `dependencies` on the diagnostic response (edge type + target);
* tells analyzers which ConfigMaps, Secrets, PVCs, and Nodes to GET;
* connects Services to EndpointSlices and selected Pods;
* notes HPA/PDB so operators (and the Safe Action Engine, independently) can
  see autoscaling and disruption constraints.

Correlation still keys off Signals collected from status, Events, logs, and
dependency existence checks. A missing Secret is a DEPENDENCY Signal, not
merely an edge.

## Known Limitations

* Ingress graph edges stop at Services; backend EndpointSlices/Pods are handled
  in the Ingress analyzer, not always as graph nodes.
* DaemonSet builder does not link HPA/PDB (DaemonSets are not HPA scale targets
  in the usual sense; PDB overlap is not attached on the DS graph path).
* Job graph `addPodsByOwner` is called with a nil selector (UID filter only).
* Generic CRD diagnosis does **not** build a graph.
* PVC diagnosis fetches PV/StorageClass without using `Builder` (no graph
  object on that path today).
* Graph construction lists namespace-wide ReplicaSets/Jobs/HPAs/PDBs; large
  namespaces hit node/depth caps first.
* Selector matching for PDBs uses label overlap helpers; matchExpressions-only
  selectors are treated conservatively (may link more PDBs than a full
  Kubernetes selector evaluation).
