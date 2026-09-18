# Architecture

kube-sre-mcp is a **deterministic Kubernetes diagnostic engine** exposed through
the Model Context Protocol (MCP). It is a single Go process that speaks MCP on
**stdio** and talks to the Kubernetes API with **client-go**.

There is **no LLM inside this server**. MCP clients such as Claude Desktop or
other compatible hosts supply natural-language reasoning and tool selection.
kube-sre-mcp itself:

* resolves Kubernetes resources;
* queries the API;
* builds a bounded Resource Graph;
* emits normalized Signals from status, Events, logs, dependencies, and optional metrics;
* correlates Signals into ranked diagnostic hypotheses;
* attaches Evidence, Confidence, Impact, Visibility, and Recommendations;
* optionally executes a small, gated Safe Action surface.

Kubernetes RBAC remains the authorization boundary. The MCP client is not treated
as an authorization authority. See [security.md](security.md).

kube-sre-mcp currently runs locally and connects to Kubernetes using your existing
kubeconfig. The Kubernetes identity and permissions are inherited directly from
the selected kubeconfig context and enforced by Kubernetes RBAC.

## Overview

```text
MCP Client
    |
    v
Go MCP Server
    |
    +--> Resource Resolution
    |
    +--> Diagnostic Engine
    |       |
    |       +--> Resource Graph
    |       +--> Health Engine
    |       +--> Event Intelligence
    |       +--> Log Intelligence
    |       +--> Metrics
    |       +--> Dependency Analysis
    |       +--> Scheduler Approximation
    |
    +--> Signal Engine
    |
    +--> Correlation Engine
    |
    +--> Root Cause Hypotheses
    |
    +--> Recommendations
    |
    +--> Safe Action Engine
    |
    v
Kubernetes API Server
```

Entry point: `cmd/kube-sre-mcp`. MCP registration: `internal/mcp`. Kubernetes
access: `internal/kube` (`ClusterClient` with typed, dynamic, discovery, RESTMapper,
and optional metrics clients). Diagnostic packages do not import the MCP SDK.

Optional process metrics bind a Prometheus HTTP listener via
`KUBE_SRE_MCP_METRICS_LISTEN`. That port is **not** an MCP transport.

## Runtime Flow

```text
User question
→ MCP tool
→ resource resolution
→ Kubernetes API queries
→ graph/dependency analysis
→ normalized signals
→ correlation
→ root cause
→ evidence/confidence/impact
→ recommendation
```

A typical diagnostic call (`k8s_diagnose_resource` and kind-specific diagnose
tools) does the following:

1. Wrap the request in `context.WithTimeout` (`KUBE_SRE_MCP_DIAGNOSTIC_TIMEOUT`, default 45s).
2. Select a cluster via kubeconfig context (process default or per-tool `context`).
3. Normalize kind aliases (`po`, `deploy`, `svc`, …) and resolve name/namespace.
4. `GET` the object. Prefix match is allowed on **read** paths only when enabled.
5. Dispatch a kind-specific analyzer, or generic `status.conditions` inspection.
6. Walk a bounded Resource Graph (owners, selectors, volumes, EndpointSlices, HPA/PDB).
7. Collect Events, logs (failing container), optional `metrics.k8s.io` usage.
8. Normalize Signals; correlate into ranked Root Cause Hypotheses.
9. Evaluate Health (`healthy` / `degraded` / `critical` / `unknown`).
10. Map the top category to Recommendations. Restart is not the default for image, auth, config, or mount failures.

Mutating tools skip the Diagnostic Engine and enter the Safe Action Engine
instead. See [safe-actions.md](safe-actions.md).

## Main Internal Packages

| Package | Responsibility |
| --- | --- |
| `internal/mcp` | MCP stdio server, tool registration, request handlers, Secret stripping, timeouts, process metrics hooks. |
| `internal/kube` | `ClusterClient` / `Factory`, RESTMapper, kind aliases, resolver (exact/prefix/ambiguous), error classification, Visibility recording. |
| `internal/diagnostic` | Kind-specific analyzers, pipeline assembly, bounded child-pod fan-out, approximate scheduler eligibility, generic CRD path. See [diagnostic-engine.md](diagnostic-engine.md). |
| `internal/graph` | Resource Graph builder: nodes, relation types, owner UID matching, depth/node caps, truncation. See [resource-graph.md](resource-graph.md). |
| `internal/signal` | Normalized Signal model: ID, source, reason, severity, confidence, resource, metadata. Source-quality ranking. |
| `internal/events` | Kubernetes Event listing (namespaced, cluster-scoped Node, all-namespaces), reason-first classification, conversion to Signals. |
| `internal/logs` | Pod log API, failing-container selection, previous logs, pattern analysis, redaction. |
| `internal/correlation` | Dedup Events, classify Signals into RCA categories, rank multiple hypotheses, attach supporting/contradicting Evidence IDs. |
| `internal/health` | Single Health evaluator used by resource diagnosis and cluster inventory. Cluster health listing + capped deep diagnosis. |
| `internal/recommendation` | Category → recommended next steps. `RestartUseful` policy used by restart preflight. |
| `internal/action` | Safe Action Engine: feature flag, SSAR, risk, PDB/HPA/last-node guards, confirmation store, UID revalidation, patch/delete. See [safe-actions.md](safe-actions.md). |
| `internal/security` | Central redaction (JWT, Bearer, passwords, PEM, dockerconfig, AWS keys) and Secret metadata-only summaries. |
| `internal/metrics` | Optional `metrics.k8s.io` client for Pod/Node usage as supporting evidence. |
| `internal/observability` | JSON `log/slog` on stderr; optional Prometheus registry (`mcp_tool_calls_total`, diagnoses, actions, API errors). |
| `internal/config` | Environment-variable configuration. No CLI flags except `--help` / `--version`. |
| `internal/model` | Shared types: Health, Confidence, Evidence, Root Cause Hypothesis, Impact, Visibility, DiagnosticResponse. |

## Data Flow

```text
Resource
→ signals
→ hypotheses
→ evidence
→ response
```

The object under diagnosis is the Resource. Analyzers emit Signals from STATUS,
CONDITION, EVENT, LOG, DEPENDENCY, SCHEDULER, STORAGE, NETWORK, METRIC, and
SECURITY sources. The Correlation Engine buckets Signals into Root Cause
Hypotheses. Each hypothesis cites Evidence IDs. The MCP handler serializes a
`DiagnosticResponse`: health, summary, impact, `root_cause` (top hypothesis),
`root_causes` (ranked list), findings, evidence, recommendations, visibility.

If Visibility is limited (403, timeout, API error), Health is not promoted to
`healthy` solely because no Signals were collected. Incomplete evidence yields
`unknown` or `partial`. Details: [diagnostic-engine.md](diagnostic-engine.md).

## Read vs Write Architecture

| Path | Tools | Kubernetes verbs | Extra gates |
| --- | --- | --- | --- |
| Diagnostic / read | diagnose, find, get, list, logs, events, cluster health, context | `get` / `list` / `watch` / `pods/log` | Prefix match (optional), timeouts, redaction |
| Write / Safe Action | restart, scale, delete pod, cordon, uncordon | `patch` or `delete` | Feature flag, SSAR, confirmation, UID re-read, PDB/HPA/last-node where applicable |

MCP annotations set `ReadOnlyHint=true` on diagnostic tools. Write tools set
`ReadOnlyHint=false` and `DestructiveHint` / `IdempotentHint` as registered.

Write tools **do not** use prefix match. Names must be exact. Diagnostic tools
never patch or delete.

## Design Principles

* **Kubernetes-native APIs** — typed, dynamic, and discovery clients; RESTMapper for unknown kinds.
* **client-go** — no shelling out to `kubectl` for normal API operations.
* **Deterministic diagnosis** — the same cluster state produces the same structured JSON.
* **Evidence-first RCA** — hypotheses cite Evidence IDs; logs alone cannot become `confirmed`.
* **Bounded concurrency** — graph depth/node caps, max deep pods, max concurrent Kubernetes calls.
* **Explicit UNKNOWN** — insufficient Visibility is `unknown`, not `healthy`.
* **Least privilege** — the kubeconfig identity’s Kubernetes RBAC is the authorization boundary; cluster-admin is not required.
* **Safe-by-default mutations** — writes are off until `KUBE_SRE_MCP_ACTIONS_ENABLED=true`.

## Concurrency and Timeouts

* Per-tool timeout: `KUBE_SRE_MCP_DIAGNOSTIC_TIMEOUT` (default 45 seconds).
* Kubernetes REST timeout: `KUBE_SRE_MCP_API_TIMEOUT_SECONDS` (default 15).
* Resource Graph: `KUBE_SRE_MCP_MAX_GRAPH_DEPTH` (6), `KUBE_SRE_MCP_MAX_GRAPH_NODES` (80).
* Workload child diagnosis: `KUBE_SRE_MCP_MAX_DEEP_PODS` (8), fan-out `KUBE_SRE_MCP_MAX_CONCURRENT_K8S` (8).
* Cluster health deep RCA: `KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES` (8).

Hitting a graph cap sets `truncated: true` and a `truncation_reason` (`max_depth` or `max_nodes`).

## Observability

JSON logs go to **stderr** so stdout remains MCP. Level:
`KUBE_SRE_MCP_LOG_LEVEL` (`debug` / `info` / `warn` / `error`).

Optional Prometheus metrics (`KUBE_SRE_MCP_METRICS_LISTEN`) are unauthenticated.
Bind to localhost unless the operator provides network protection. The listener
shuts down when the MCP process exits.

## Deployment

* Local binary on stdio with kubeconfig authentication is the supported runtime for v0.1.0.
* A `Dockerfile` is provided for optional local image builds. v0.1.0 does not publish images to GHCR or any other registry.
* In-cluster ServiceAccount authentication is **not** supported in this release.
* Container publishing and in-cluster execution are planned for a future release.

## Non-Goals

kube-sre-mcp is **not**:

* a complete kube-scheduler implementation (Pending-pod analysis is approximate eligibility; see [diagnostic-engine.md](diagnostic-engine.md));
* an autonomous remediation agent (no post-action verification loop, no change windows, no unattended mutation);
* a replacement for Prometheus, metrics-server, or APM (metrics are optional supporting evidence);
* a generic unrestricted `kubectl` execution layer (no `exec`, no arbitrary patch/apply/delete YAML);
* an HTTP/SSE/Streamable HTTP MCP server.

## Known Limitations

* Prefix match is conservative (prefix, not silent substring). Ambiguous matches are listed, not guessed. Writes require exact names.
* Cluster health inventories nodes, pods, deployments, statefulsets, daemonsets, jobs, and PVCs, then deep-diagnoses a **capped** set of unhealthy candidates.
* Generic CRDs inspect `status.conditions` only (`diagnostic_depth: generic`). Domain-specific CRD analyzers are not implemented.
* Required pod anti-affinity and topology spread are reported as unsupported scheduler constraints.
* Node affinity `Gt` / `Lt` operators are not evaluated.
* There is no identity-aware approval, policy engine, or multi-party confirmation. Those are roadmap items in [security.md](security.md).
