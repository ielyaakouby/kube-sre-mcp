# Kube SRE MCP tool catalog

Canonical list of MCP tools registered by Kube SRE MCP in `internal/mcp` (`Register` in `internal/mcp/handlers.go`). The live server exposes **18** tools over **stdio**.

There is no HTTP, SSE, or Streamable HTTP MCP transport.

```
AI / MCP client (stdio)
        │
        ▼
   Kube SRE MCP (cmd/kube-sre-mcp)
        │
        ▼
   internal/mcp  (tool registration + handlers)
        │
        ├── Diagnostic Engine / Resource Graph / Signals / Correlation / Recommendations
        ├── Events, Logs, Cluster Health
        ├── Find / Get / List
        └── Action Engine (writes; disabled by default)
                │
                ▼
        client-go ClusterClient
                │
                ▼
        Kubernetes API Server  →  Kubernetes RBAC
```

Documentation index: [docs/README.md](docs/README.md).
Architecture: [docs/architecture.md](docs/architecture.md).
Diagnostic Engine: [docs/diagnostic-engine.md](docs/diagnostic-engine.md).
Resource Graph: [docs/resource-graph.md](docs/resource-graph.md).
Security: [docs/security.md](docs/security.md).
Safe Actions: [docs/safe-actions.md](docs/safe-actions.md).
Configuration: [docs/configuration.md](docs/configuration.md).
Troubleshooting: [docs/troubleshooting.md](docs/troubleshooting.md).
RBAC: [deploy/rbac.md](deploy/rbac.md).

**Modes**

| Mode | Meaning |
| --- | --- |
| READ-ONLY | Kubernetes get/list (and logs) only. MCP `ReadOnlyHint=true`. |
| DIAGNOSTIC | Read-only diagnosis: graph, events, logs, optional metrics, ranked RCA. |
| OBSERVABILITY | Read-only logs or events. |
| MUTATING / ACTION | Changes cluster state. Requires `KUBE_SRE_MCP_ACTIONS_ENABLED=true`, SelfSubjectAccessReview, and a confirmation token. |

Writes are **disabled** unless `KUBE_SRE_MCP_ACTIONS_ENABLED=true`. A first mutating call without `confirmation_id` returns `status: confirmation_required` and a single-use token (TTL from `KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS`, default 120s). The second call must repeat the same target/parameters plus that id.

kube-sre-mcp uses a two-step confirmation protocol for mutating actions. The MCP host/client is responsible for presenting the confirmation request to the user and must not automatically submit confirmation IDs without user approval. Server confirmation is not proof of human interaction. Kubernetes RBAC remains the final authorization boundary.

Read tools may resolve names by **prefix** when `KUBE_SRE_MCP_PREFIX_MATCH=true` (default). Ambiguous matches are listed, not guessed. **Writes require exact resource names.**

Optional `context` on every tool selects a kubeconfig context via the client factory.

---

## Summary

| Tool | Category | Mode | Purpose |
| --- | --- | --- | --- |
| `k8s_diagnose_resource` | Diagnostics | DIAGNOSTIC | Diagnose any kind by name (primary tool) |
| `k8s_diagnose_pod` | Diagnostics | DIAGNOSTIC | Deep Pod diagnosis |
| `k8s_diagnose_deployment` | Diagnostics | DIAGNOSTIC | Deployment + ReplicaSet + Pod RCA |
| `k8s_diagnose_service` | Diagnostics | DIAGNOSTIC | Service selector, EndpointSlices, backends |
| `k8s_diagnose_node` | Diagnostics | DIAGNOSTIC | Node conditions, taints, cordon, events |
| `k8s_find_resource` | Inspection | READ-ONLY | Find by name/prefix across namespaces |
| `k8s_get_resource` | Inspection | READ-ONLY | Get a summarized resource (Secrets stripped) |
| `k8s_list_resources` | Inspection | READ-ONLY | List a kind with selectors |
| `k8s_get_logs` | Observability | OBSERVABILITY | Pod logs (redacted) |
| `k8s_get_events` | Observability | OBSERVABILITY | Kubernetes Events |
| `k8s_cluster_health` | Health | DIAGNOSTIC | Cluster inventory and aggregated health |
| `k8s_get_context` | Context | READ-ONLY | Current kube context, API server, namespace |
| `k8s_list_contexts` | Context | READ-ONLY | kubeconfig contexts |
| `k8s_restart_deployment` | Actions | MUTATING | Rollout-restart a Deployment |
| `k8s_scale_workload` | Actions | MUTATING | Scale Deployment / StatefulSet / ReplicaSet |
| `k8s_delete_pod` | Actions | MUTATING | Delete a Pod |
| `k8s_cordon_node` | Actions | MUTATING | Cordon a Node |
| `k8s_uncordon_node` | Actions | MUTATING | Uncordon a Node |

---

### k8s_diagnose_resource

Purpose:
Primary diagnostic tool. Resolves the object, builds a resource graph, collects status/events/logs/dependencies (and metrics when `metrics.k8s.io` is available), correlates signals, and returns ranked root-cause hypotheses with evidence, impact, and recommendations.

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Kind-specific analyzers for Pod, Deployment, StatefulSet, DaemonSet, Job, CronJob, Service, Ingress, PersistentVolumeClaim, Node. Other kinds (including CRDs reachable via discovery/dynamic client) use generic `status.conditions` inspection (`diagnostic_depth: generic`). Kind aliases such as `po`, `deploy`, `svc`, `sts`, `ds`, `ing`, `pvc` are accepted.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `kind` | yes | Resource kind or alias |
| `name` | yes | Resource name or prefix |
| `namespace` | no | Auto-resolved when omitted |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list`/`watch` on the target kind and related graph objects (Pods, ReplicaSets, Events, ConfigMaps, Secrets metadata, PVCs, Services, EndpointSlices, Nodes, HPAs, PDBs, StorageClasses as applicable). `get` on `pods/log` when logs are collected. Optional `get`/`list` on `metrics.k8s.io` pods/nodes.

Expected result:
Structured diagnostic JSON: `health` (`healthy` / `degraded` / `critical` / `unknown`), findings, evidence, `root_cause`, recommendations, visibility/RBAC gaps. Permission failures are `unknown`, not healthy.

Example prompts:
- "Why is pod checkout-api-xxxx failing?"
- "Diagnose Deployment payments in namespace production."
- "Diagnose Ingress public-web."

Related tools:
`k8s_diagnose_pod`, `k8s_diagnose_deployment`, `k8s_diagnose_service`, `k8s_diagnose_node`, `k8s_get_logs`, `k8s_get_events`

---

### k8s_diagnose_pod

Purpose:
Deep-diagnose a Pod: container status, probes, scheduling eligibility (approximate, not kube-scheduler), volume/config dependencies, events, logs, RCA.

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Pod

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Pod name or prefix |
| `namespace` | no | Auto-resolved when omitted |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list` Pods, Events, ConfigMaps, Secrets (metadata), PVCs, Nodes; `get` `pods/log`; optional pod metrics.

Example prompts:
- "Diagnose pod checkout-xxxx in production."
- "Why is this pod in CrashLoopBackOff?"

Related tools:
`k8s_diagnose_resource`, `k8s_get_logs`, `k8s_get_events`

---

### k8s_diagnose_deployment

Purpose:
Diagnose a Deployment including ReplicaSets and unhealthy Pods; aggregated workload RCA.

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Deployment

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Deployment name or prefix |
| `namespace` | no | Auto-resolved when omitted |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list` Deployments, ReplicaSets, Pods, Events; same follow-on permissions as pod diagnosis for capped deep-pod analysis (`KUBE_SRE_MCP_MAX_DEEP_PODS`).

Example prompts:
- "Why is Deployment payment-api unavailable?"
- "Rollout of frontend looks stuck. Diagnose it."

Related tools:
`k8s_diagnose_resource`, `k8s_diagnose_pod`, `k8s_restart_deployment`

---

### k8s_diagnose_service

Purpose:
Diagnose a Service: selector, EndpointSlices, ports, and selected Pod health.

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Service

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Service name or prefix |
| `namespace` | no | Auto-resolved when omitted |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list` Services, EndpointSlices (and/or Endpoints), Pods, Events.

Example prompts:
- "Why doesn’t Service checkout have endpoints?"
- "Diagnose Service payments in production."

Related tools:
`k8s_diagnose_resource`, `k8s_diagnose_pod`

---

### k8s_diagnose_node

Purpose:
Diagnose a Node: conditions, taints, cordon (`unschedulable`), events, optional node metrics.

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Node (cluster-scoped)

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Node name |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list` Nodes, Events (cluster-wide for Node objects); optional `metrics.k8s.io` nodes.

Example prompts:
- "Diagnose node worker-3."
- "Check node pressure on worker-3."

Related tools:
`k8s_cluster_health`, `k8s_cordon_node`, `k8s_uncordon_node`

---

### k8s_find_resource

Purpose:
Find a resource by name/query across namespaces. Prefix match is controlled by `KUBE_SRE_MCP_PREFIX_MATCH`. Ambiguous matches are returned rather than guessed.

Mode:
READ-ONLY

Supported resources:
Any kind the resolver/discovery can list (built-in aliases plus CRDs via RESTMapper).

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `kind` | yes | Resource kind or alias |
| `query` | yes | Name or prefix to search |
| `namespace` | no | Limit to one namespace |
| `all_namespaces` | no | Search all namespaces |
| `context` | no | kubeconfig context |

RBAC:
`list` (and `get` for the unique match) on the requested kind.

Example prompts:
- "Find pod payment-api-7d9f — I don’t know the namespace."
- "Where is Deployment checkout?"

Related tools:
`k8s_get_resource`, `k8s_diagnose_resource`

---

### k8s_get_resource

Purpose:
Get a summarized resource. Secrets return metadata and key names only — never `.data` / `.stringData` values.

Mode:
READ-ONLY

Supported resources:
Any resolvable kind. Summaries are richer for Pod, Deployment, Service, Node, PVC, Secret.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `kind` | yes | Resource kind |
| `name` | yes | Resource name or prefix |
| `namespace` | no | Auto-resolved when omitted |
| `context` | no | kubeconfig context |

RBAC:
`get`/`list` on the kind.

Example prompts:
- "Describe deployment frontend."
- "Get Secret regcred in production (keys only)."

Related tools:
`k8s_list_resources`, `k8s_find_resource`

---

### k8s_list_resources

Purpose:
List resources of a kind. Honors `label_selector` and `field_selector`. Results are capped at 200 items (`truncated: true` when more exist). Default namespace is the kubeconfig context namespace unless `all_namespaces` is set. Cluster-scoped kinds ignore namespace.

Mode:
READ-ONLY

Supported resources:
Any listable kind via typed or dynamic client.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `kind` | yes | Resource kind |
| `namespace` | no | Namespace (default: current) |
| `all_namespaces` | no | List across namespaces |
| `label_selector` | no | Kubernetes label selector |
| `field_selector` | no | Kubernetes field selector |
| `context` | no | kubeconfig context |

RBAC:
`list` on the kind.

Example prompts:
- "List Deployments in production."
- "List Pods in kube-system with label k8s-app=kube-dns."

Related tools:
`k8s_get_resource`, `k8s_cluster_health`

---

### k8s_get_logs

Purpose:
Get Pod logs via the Kubernetes API. If `container` is omitted, a failing container is selected when the Pod can be resolved. Lines are redacted (tokens, passwords, PEMs). Handler default tail is 100 lines, max 500.

Mode:
OBSERVABILITY (read-only)

Supported resources:
Pod

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `pod` | yes | Pod name |
| `namespace` | no | Default: current context namespace |
| `container` | no | Container name; otherwise failing-container selection |
| `previous` | no | Previous terminated container logs |
| `tail_lines` | no | Tail length (default 100, max 500) |
| `since_seconds` | no | Only logs newer than this many seconds |
| `timestamps` | no | Include timestamps (default true) |
| `context` | no | kubeconfig context |

RBAC:
`get` Pods, `get` `pods/log`.

Example prompts:
- "Show the last 80 logs for pod api-xxx."
- "Previous logs for the crash-looping container on worker in staging."

Related tools:
`k8s_diagnose_pod`

---

### k8s_get_events

Purpose:
Get Kubernetes Events. Supports filtering by resource name/kind, Warning-only, Node (cluster-scoped), and all-namespaces. List failures are returned as errors (not empty success). Handler default limit 30, max 100.

Mode:
OBSERVABILITY (read-only)

Supported resources:
Events associated with any involved object (Pods, Deployments, Nodes, …).

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `namespace` | no | Default: current context namespace |
| `resource_name` | no | `involvedObject.name` |
| `resource_kind` | no | `involvedObject.kind` |
| `all_namespaces` | no | Cluster-wide event list |
| `warnings_only` | no | Keep `type=Warning` only |
| `limit` | no | Max events (default 30, max 100) |
| `context` | no | kubeconfig context |

RBAC:
`list` Events in the target namespace or cluster-wide.

Example prompts:
- "Get events for deployment payments in namespace production."
- "Warning events for node worker-3."

Related tools:
`k8s_diagnose_resource`, `k8s_cluster_health`

---

### k8s_cluster_health

Purpose:
Cluster health inventory: nodes, pods, deployments, statefulsets, daemonsets, jobs, PVCs. Aggregates HEALTHY / DEGRADED / CRITICAL / UNKNOWN. Optionally deep-diagnoses a capped set of unhealthy candidates (`KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES`).

Mode:
DIAGNOSTIC (read-only)

Supported resources:
Cluster-wide or single-namespace inventory of the kinds listed above.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `namespace` | no | Limit inventory to one namespace (nodes remain cluster-scoped) |
| `include_healthy` | no | Include healthy resource summaries |
| `max_problems` | no | Cap reported problems (default 50) |
| `context` | no | kubeconfig context |

RBAC:
`list` Nodes, Pods, Deployments, StatefulSets, DaemonSets, Jobs, PersistentVolumeClaims; plus permissions used by deep diagnosis when it runs. Missing list permission marks that area `unknown`, not healthy.

Example prompts:
- "Is my cluster healthy?"
- "Show me unhealthy workloads in production."

Related tools:
`k8s_diagnose_resource`

---

### k8s_get_context

Purpose:
Return the current kubeconfig context name, API server URL, and namespace for the selected cluster client.

Mode:
READ-ONLY

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `context` | no | kubeconfig context to inspect |

RBAC:
None beyond the credentials already used to construct the client.

Example prompts:
- "Which Kubernetes cluster and namespace are you talking to?"

Related tools:
`k8s_list_contexts`

---

### k8s_list_contexts

Purpose:
List kubeconfig contexts and the current context name.

Mode:
READ-ONLY

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `context` | no | Used only to select which loaded config to read |

RBAC:
Local kubeconfig; no extra API verbs.

Example prompts:
- "List kubeconfig contexts."

Related tools:
`k8s_get_context`

---

## Mutating / action tools

**Warning:** these tools modify the cluster when enabled.

Common statuses: `disabled`, `forbidden`, `confirmation_required`, `confirmation_mismatch`, `preflight_failed`, `blocked_by_pdb`, `restart_not_recommended`, `error`, `success`.

Kubernetes RBAC remains the final authorization boundary. The server also runs SelfSubjectAccessReview before mutating.

---

### k8s_restart_deployment

Purpose:
Rollout-restart a Deployment by patching `kubectl.kubernetes.io/restartedAt` on the pod template.

Mode:
MUTATING / ACTION (`ReadOnlyHint=false`, not marked destructive)

Supported resources:
Deployment only. Exact name required.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Deployment name (exact) |
| `namespace` | yes | Namespace |
| `confirmation_id` | no | Token from a prior `confirmation_required` response |
| `context` | no | kubeconfig context used for the write |
| `reason_category` | no | Optional RCA category; not trusted blindly |
| `force` | no | Override `restart_not_recommended` |

Safety:
SSAR `patch` on `deployments`. PDB `disruptionsAllowed=0` blocks. UID must match between confirm and execute. Restart is refused (unless `force`) for categories such as image pull / missing config / failed mount. Confirmation required.

RBAC:
`get`/`patch` Deployments; `list` PodDisruptionBudgets; `create` SelfSubjectAccessReviews; `list` Pods (category inference).

Example prompts:
- "Restart deployment api in namespace staging." (enable writes first; confirm the token)

Related tools:
`k8s_diagnose_deployment`

---

### k8s_scale_workload

Purpose:
Scale Deployment, StatefulSet, or ReplicaSet by patching `spec.replicas`. Rejects negative replicas and values above `KUBE_SRE_MCP_MAX_REPLICAS` (default 100). HPA targeting the workload is a critical preflight warning; an HPA appearing after confirmation fails closed.

Mode:
MUTATING / ACTION (`IdempotentHint=true`)

Supported resources:
Deployment, StatefulSet, ReplicaSet (kind aliases accepted). Exact name required.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `kind` | yes | `Deployment`, `StatefulSet`, or `ReplicaSet` |
| `name` | yes | Workload name (exact) |
| `namespace` | yes | Namespace |
| `replicas` | yes | Desired replica count |
| `confirmation_id` | no | Confirmation token |
| `context` | no | kubeconfig context |

Safety:
SSAR `patch` on the resource. Confirmation binds replicas. UID check at execute time.

RBAC:
`get`/`patch` deployments, statefulsets, or replicasets; `list` HPAs; `create` SelfSubjectAccessReviews.

Example prompts:
- "Scale deployment worker to 5 replicas."

Related tools:
`k8s_diagnose_deployment`

---

### k8s_delete_pod

Purpose:
Delete a Pod with a grace period (handler default 30s when `0`/omitted).

Mode:
MUTATING / ACTION (`DestructiveHint=true`)

Supported resources:
Pod. Exact name required.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Pod name (exact) |
| `namespace` | yes | Namespace |
| `grace_period_seconds` | no | Grace period (default 30) |
| `confirmation_id` | no | Confirmation token |
| `context` | no | kubeconfig context |

Safety:
Refuses control-plane/system pods and static/mirror pods. PDB `disruptionsAllowed=0` blocks. Unmanaged pods, DaemonSet/Job-owned pods, and `kube-system` are higher risk (still require confirmation, not a hard refuse except control-plane/mirror). UID check at execute.

RBAC:
`get`/`delete` Pods; `list` PodDisruptionBudgets; `create` SelfSubjectAccessReviews.

Example prompts:
- "Delete pod web-xxxx in default so the controller recreates it."

Related tools:
`k8s_diagnose_pod`

---

### k8s_cordon_node

Purpose:
Cordon a Node (`spec.unschedulable=true`).

Mode:
MUTATING / ACTION (`IdempotentHint=true`)

Supported resources:
Node. Exact name required.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Node name |
| `confirmation_id` | no | Confirmation token |
| `context` | no | kubeconfig context |

Safety:
Refuses to cordon the last Ready schedulable node. Control-plane nodes are flagged critical but can proceed after confirmation. UID check at execute.

RBAC:
`get`/`list`/`patch` Nodes; `create` SelfSubjectAccessReviews.

Example prompts:
- "Cordon worker-3 for maintenance."

Related tools:
`k8s_diagnose_node`, `k8s_uncordon_node`

---

### k8s_uncordon_node

Purpose:
Uncordon a Node (`spec.unschedulable=false`).

Mode:
MUTATING / ACTION (`IdempotentHint=true`)

Supported resources:
Node. Exact name required.

Parameters:

| Name | Required | Description |
| --- | --- | --- |
| `name` | yes | Node name |
| `confirmation_id` | no | Confirmation token |
| `context` | no | kubeconfig context |

Safety:
SSAR `patch` on nodes, confirmation, UID check. No last-node restriction (scheduling is being re-enabled).

RBAC:
`get`/`patch` Nodes; `create` SelfSubjectAccessReviews.

Example prompts:
- "Uncordon worker-3."

Related tools:
`k8s_cordon_node`, `k8s_diagnose_node`

---

## Run

```bash
make build
./bin/kube-sre-mcp --help
./bin/kube-sre-mcp
```

## License

Kube SRE MCP is licensed under the Apache License 2.0 (`Apache-2.0`). See [LICENSE](LICENSE).
