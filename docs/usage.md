# Usage

kube-sre-mcp is an MCP server over **stdio**. It diagnoses Kubernetes
resources and, when enabled, runs a small set of gated mutations. It does not
embed an LLM.

Tool schemas: [CATALOG.md](../CATALOG.md). Prompts: [prompt-examples.md](prompt-examples.md).
Architecture: [architecture.md](architecture.md).

## Running kube-sre-mcp

Go 1.26.6+ is required **to build from source**. A GitHub Release binary does not
need a Go toolchain.

```bash
git clone https://github.com/ielyaakouby/kube-sre-mcp.git
cd kube-sre-mcp
make build
./bin/kube-sre-mcp --help
```

Cluster credentials via kubeconfig:

* `KUBECONFIG` if set
* otherwise `~/.kube/config`
* `KUBE_SRE_MCP_CONTEXT` optional
* otherwise kubeconfig `current-context`

**v0.1.0 does not support in-cluster ServiceAccount authentication.**

The server blocks on stdin/stdout.

`KUBE_SRE_MCP_ACTIONS_ENABLED=false` keeps mutating tools (`k8s_restart_deployment`,
`k8s_scale_workload`, `k8s_delete_pod`, `k8s_cordon_node`, `k8s_uncordon_node`)
disabled. They still register, but return `status: disabled` instead of changing
the cluster. This is the default; set `true` only when you intend to mutate.

```bash
export KUBECONFIG="$HOME/.kube/config"
export KUBE_SRE_MCP_LOG_LEVEL=info
export KUBE_SRE_MCP_ACTIONS_ENABLED=false
./bin/kube-sre-mcp
```

Equivalent: `go run ./cmd/kube-sre-mcp` or `make run`.

Installer: `bash install/install.sh` ([install/README.md](../install/README.md)).

Authentication is kubeconfig only (`KUBECONFIG`, then `~/.kube/config`).
Optional `KUBE_SRE_MCP_CONTEXT` selects a kubeconfig context; otherwise the
kubeconfig current context is used.

## Connecting an MCP Client

Transport is stdio only in v0.1.0. HTTP, SSE, and streamable HTTP are not supported.
Point the host at the **absolute** binary path.

`KUBE_SRE_MCP_CONTEXT` is **optional**. Omit it to use the kubeconfig `current-context`.

```json
{
  "mcpServers": {
    "kube-sre-mcp": {
      "command": "/absolute/path/to/kube-sre-mcp",
      "env": {
        "KUBECONFIG": "/home/you/.kube/config",
        "KUBE_SRE_MCP_LOG_LEVEL": "info",
        "KUBE_SRE_MCP_ACTIONS_ENABLED": "false"
      }
    }
  }
}
```

Do not put cluster tokens in the config file.

## Recommended workflow

1. `k8s_get_context` if more than one cluster is possible.
2. `k8s_cluster_health` for an inventory of problems.
3. `k8s_diagnose_resource` (or kind-specific diagnose tools) for *why*.
4. `k8s_find_resource` when the namespace is unknown.
5. Mutate only after diagnosis, with writes enabled. kube-sre-mcp uses a
   two-step confirmation protocol; the MCP host must present the confirmation
   request to the user and must not automatically submit `confirmation_id`.

Prefer **diagnose** over get/list when something is broken.

## Basic Resource Diagnosis

Ask why a named object is unhealthy. The host should call
`k8s_diagnose_resource` with `kind` + `name` (+ `namespace` if known).

Kind aliases include `po`, `deploy`, `svc`, `sts`, `ds`, `ing`, `pvc`, `job`,
`cronjob`, `node`.

Expect Health, Evidence, a top Root Cause Hypothesis, Impact, Recommendations,
and Visibility. See [Expected Response Concepts](#expected-response-concepts).

## Pod Diagnosis

Use `k8s_diagnose_pod` or `k8s_diagnose_resource` with `kind=Pod`.

Covers waiting reasons (ImagePullBackOff, CrashLoopBackOff, …), OOMKilled,
probes, missing ConfigMap/Secret/PVC, node conditions, Events, redacted logs,
optional metrics, and approximate scheduling eligibility for Pending Pods.

## Deployment Diagnosis

Use `k8s_diagnose_deployment` or diagnose `kind=Deployment`.

Aggregates current ReplicaSet Pods (old ReplicaSets excluded from current RCA),
replica Impact, `ProgressDeadlineExceeded`, and capped deep-diagnosis of
unhealthy Pods.

StatefulSet, DaemonSet, Job, and CronJob use `k8s_diagnose_resource` with that
kind (no dedicated MCP wrappers).

## Service / Ingress Diagnosis

* `k8s_diagnose_service` — selector, EndpointSlices, Endpoints fallback, selected Pod health, port mismatch.
* `k8s_diagnose_resource` with `kind=Ingress` — backend Services, TLS Secret **existence**, IngressClass, ready endpoints.

ExternalName Services are described and not treated as missing selectors.

## Cluster Health

`k8s_cluster_health` inventories nodes, pods, deployments, statefulsets,
daemonsets, jobs, and PVCs. Optional `namespace`, `include_healthy`,
`max_problems` (default 50).

It deep-diagnoses a **capped** set of unhealthy candidates. Permission gaps
are `unknown`, not healthy. Details: [diagnostic-engine.md](diagnostic-engine.md).

## Logs

`k8s_get_logs`: Pod logs via the API, failing-container auto-select, redaction.
Parameters: `pod`, optional `namespace`, `container`, `previous`, `tail_lines`
(default 100, max 500), `since_seconds`, `timestamps`, `context`.

Diagnose already pulls a bounded log sample when a failing container exists.
Use this tool when you need a longer tail or previous logs explicitly.

## Events

`k8s_get_events`: namespaced, all-namespaces, or Node cluster-scoped.
Optional `resource_name` / `resource_kind`, `warnings_only`, `limit` (default
30, max 100). List failures are returned as errors, not empty success.

## Generic Resource Access

| Tool | Use |
| --- | --- |
| `k8s_find_resource` | `kind` + `query`; optional namespace / `all_namespaces`. Ambiguous matches listed. |
| `k8s_get_resource` | Summarized get. Secrets: metadata and key names only. |
| `k8s_list_resources` | Kind + optional selectors. Caps serialized items at 200 (`truncated` if more). |

These are inventory tools. They do not run the Diagnostic Engine.

## Mutating Actions

Five tools: `k8s_restart_deployment`, `k8s_scale_workload`, `k8s_delete_pod`,
`k8s_cordon_node`, `k8s_uncordon_node`.

Off until `KUBE_SRE_MCP_ACTIONS_ENABLED=true`. Exact names only. Full
semantics: [safe-actions.md](safe-actions.md).

## Confirmation Workflow

kube-sre-mcp uses a two-step confirmation protocol for mutating actions. The MCP
host/client is responsible for presenting the confirmation request to the user
and must not automatically submit confirmation IDs without user approval.
kube-sre-mcp provides server-side confirmation semantics (bind, TTL, single-use,
UID check). That is **not** proof that a human approved. Kubernetes RBAC remains
the final authorization boundary.

1. Call the write tool without `confirmation_id`.
2. Read `status: confirmation_required`, `risk`, `blast_radius`, `preconditions`.
3. The MCP host should obtain user approval (it must not auto-confirm).
4. Repeat the same arguments with `confirmation_id`.

Mismatch, expiry, or UID change refuses the mutation. PDB/HPA/last-node
guards still apply on the second call.

## Context Selection

`k8s_get_context` — current context, API server, namespace.
`k8s_list_contexts` — kubeconfig context names.

Every tool accepts optional `context`. Writes bind confirmation to that
context so a second call cannot silently target another cluster.

## Working with CRDs

If discovery can map the kind, `k8s_diagnose_resource` runs **generic**
inspection of `status.conditions` (`diagnostic_depth: generic`). No
domain-specific analyzer. Empty conditions → Health `unknown`.

The kubeconfig identity must be granted get/list on those CRD **instances**.
Grant those resources separately. `k8s_get_resource` / `k8s_list_resources`
use the same dynamic client.

## Expected Response Concepts

| Field | Meaning |
| --- | --- |
| `health` | `healthy` / `degraded` / `critical` / `unknown` |
| `root_cause` | Top Root Cause Hypothesis (cleared when healthy) |
| `root_causes` | Ranked hypotheses with categories and Evidence IDs |
| `confidence` | `low` / `medium` / `high` / `confirmed` on each hypothesis |
| `impact` | Replica, endpoint, or node blast (when the analyzer sets it) |
| `evidence` | ID, source, message, resource, timestamp |
| `recommended_actions` | Next steps; restart is not default for image/auth/config/mount |
| `visibility` | `complete` / `limited`, missing permissions, failed checks |
| `status` | `success` / `partial` / `error` (diagnose); write statuses documented in [safe-actions.md](safe-actions.md) |
| `metrics_available` | Optional `metrics.k8s.io` |
| `truncated` | Graph or deep-pod cap hit |

Logs alone do not create `confirmed` confidence. Missing RBAC does not create
`healthy`.

## Tests

```bash
make test
make test-race
```

Go tests under `internal/*` use fake client-go objects. A live cluster is not
required.

## Common errors

| Error | Cause | Fix |
| --- | --- | --- |
| no usable kubeconfig at startup | no cluster credentials | set `KUBECONFIG` or configure `~/.kube/config` |
| `status: disabled` on write tools | `KUBE_SRE_MCP_ACTIONS_ENABLED=false` | enable only when operator RBAC is in place |
| `confirmation_required` | Action Engine confirmation gate | host presents confirmation; retry with `confirmation_id` only after user approval |
| `confirmation_mismatch` | Token reused, bind failed, or UID changed | Start the action again |
| `forbidden` | Kubernetes RBAC or SSAR deny | Grant the verb; see [deploy/rbac.md](../deploy/rbac.md) |
| health `unknown` | RBAC / API error | grant read diagnostic RBAC; do not treat as healthy |

More operator cases: [troubleshooting.md](troubleshooting.md).
