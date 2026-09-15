# Security Architecture

This document describes the **runtime** security model of kube-sre-mcp.

Vulnerability reporting, supported versions, and disclosure process:
[SECURITY.md](../SECURITY.md). Recommended kubeconfig RBAC: [deploy/rbac.md](../deploy/rbac.md).
Mutating tool semantics: [safe-actions.md](safe-actions.md).

## Security Philosophy

> The MCP client/LLM is not treated as an authorization authority.

An AI client may:

* request;
* diagnose;
* propose;
* plan.

It must not be trusted to authorize itself. Kubernetes RBAC of the process
identity (the selected kubeconfig user) is the final allow/deny. Extra
application gates (feature flag, SSAR, confirmation, UID re-read) exist because
an LLM can pick the wrong tool, the wrong object, or replay a stale plan.

## Defense in Depth

Layers that exist **today** (write path; order matches `internal/action`):

```text
MCP Client / AI
      ↓
Tool contract
      ↓
Actions enabled?
      ↓
SSAR
      ↓
Target GET + action policy / safety checks
      ↓
Risk analysis
      ↓
Confirmation
      ↓
State/UID revalidation (PDB/HPA/last-node re-check)
      ↓
Kubernetes API (RBAC enforced again)
```

| Layer | Implemented? | Where |
| --- | --- | --- |
| Tool contract (fixed 18 tools, typed args, ReadOnlyHint) | Yes | `internal/mcp` |
| Actions disabled by default | Yes | `KUBE_SRE_MCP_ACTIONS_ENABLED` |
| Exact-name writes (no prefix) | Yes | Action Engine |
| Risk / blast-radius fields on confirmation | Yes | `internal/action` |
| Confirmation tokens | Yes | `internal/action/confirmation.go` |
| SelfSubjectAccessReview (SSAR) | Yes (writes) | `internal/action/authorization.go` |
| UID / state revalidation | Yes (writes) | before patch/delete |
| PDB / HPA / last-node / mirror-pod guards | Yes, per action | [safe-actions.md](safe-actions.md) |
| Kubernetes RBAC | Yes | API server |
| Secret `.data` stripping + redaction | Yes | `internal/mcp`, `internal/security` |
| Identity-aware human approvals (IdP, ticketing) | **Planned / future security layer** | not implemented |
| External policy engine (OPA/Kyverno-style in-process) | **Planned / future security layer** | not implemented |
| Protected-resource denylist | **Planned / future security layer** | not implemented (hard-coded control-plane/mirror/last-node only) |
| Durable audit log | **Planned / future security layer** | stderr JSON + optional Prometheus only |
| Multi-party approval | **Planned / future security layer** | not implemented |
| Admission policy integration | **Planned / future security layer** | cluster admission still applies as usual; kube-sre-mcp does not configure it |

Do not treat roadmap rows as current functionality.

## Read-Only vs Write Operations

| | Read / diagnostic | Write / Safe Action |
| --- | --- | --- |
| Threat | Over-disclosure, incomplete Visibility treated as healthy, Secret leakage | Unintended mutation, wrong object, stale confirmation, privilege confusion |
| Tools | diagnose, find, get, list, logs, events, cluster health, context | restart, scale, delete pod, cordon, uncordon |
| Prefix match | Allowed when `KUBE_SRE_MCP_PREFIX_MATCH=true` | Never |
| SSAR | Not used | Required before mutation |
| Confirmation | No | Yes |
| Default | Always registered and executable (subject to RBAC) | `status: disabled` until the feature flag is true |

Read tools never patch or delete. They can still **read** Secret objects if
RBAC allows `get`; handlers return metadata and key names only.

## RBAC

kube-sre-mcp authenticates with kubeconfig only. Recommended permission sets
for that identity are documented in [deploy/rbac.md](../deploy/rbac.md):

* **readOnly** — `get`/`list`/`watch` on diagnostic resources, Secret
  `get`/`list`, `create` SelfSubjectAccessReview, metrics.k8s.io get/list, CRD
  *definition* get/list.
* **operator** — additional `patch` on deployments/statefulsets/replicasets and
  nodes; `delete` on pods.

Least privilege: use a read-only kubeconfig identity and keep
`KUBE_SRE_MCP_ACTIONS_ENABLED=false` unless writes are required. kube-sre-mcp
cannot grant itself extra verbs. `cluster-admin` is not required.

CRD **instance** get/list is not in the default ClusterRole. Grant those
resources separately if you diagnose custom kinds.

## SelfSubjectAccessReview

Write operations call `authorization.k8s.io` `SelfSubjectAccessReview` **before**
the target GET and confirmation, for the verb they are about to use
(`patch` deployments, `delete` pods, `patch` nodes, …).

* SSAR **create error** → action `status: error` (fail closed; the mutation is
  not attempted).
* SSAR **not allowed** → `status: forbidden`, `authorized: false`.
* SSAR **allowed** → continue to preflight and confirmation.

SSAR is an extra check, not a substitute for RBAC. The API server still
enforces the same identity on the actual patch/delete.

Read tools do not call SSAR; a 403 on get/list becomes Visibility-limited
diagnosis or an error payload.

## Confirmation Tokens

Implemented in `internal/action.Store`. Properties that are actually enforced:

| Property | Behavior |
| --- | --- |
| Short-lived | TTL `KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS` (default 120). Expired tokens error (`confirmation_expired` message, `status: error`). |
| Single-use | `Take` deletes the token. Replay looks like not found (`confirmation_not_found`). |
| Action-bound | `restart_deployment` / `scale` / `delete_pod` / `cordon` / `uncordon` |
| Target-bound | kind, name, namespace |
| UID-bound | object UID captured at plan time; compared again at execution |
| Context-bound | kubeconfig context name |
| Parameter-bound | scale replicas; delete grace period; cordon bool |

Tokens are 16 random bytes, hex-encoded. They live **in process memory only**.
Restarting kube-sre-mcp invalidates pending confirmations.

A bind mismatch returns `status: confirmation_mismatch`.

kube-sre-mcp uses a two-step confirmation protocol for mutating actions. The MCP
host/client is responsible for presenting the confirmation request to the user
and must not automatically submit confirmation IDs without user approval.
Server-side confirmation is **not** proof of human interaction. Kubernetes RBAC
remains the final authorization boundary.

## Revalidation

Immediately before mutate, the engine:

* **Re-GETs** the target;
* refuses if **UID** changed (`confirmation_mismatch` — object was recreated);
* **re-checks PDB** for restart and delete-pod;
* **re-checks HPA** for scale (fail closed if HPA cannot be listed; refuse if an
  HPA appeared after confirmation);
* **re-checks last Ready schedulable node** for cordon.

## Actions Disabled by Default

`KUBE_SRE_MCP_ACTIONS_ENABLED` defaults to `false`. When false, mutating tools still **register** (so MCP
clients can see them) but return `status: disabled` without calling the API to
mutate.

## Secrets

* `k8s_get_resource` on Secret returns `security.SecretSummary`: name,
  namespace, type, **key names**, creationTimestamp. Never `.data` or
  `stringData` values.
* Diagnostic Secret inspection records existence and key names for
  CreateContainerConfigError correlation; values are not placed in Evidence.
* Logs, Events, and error strings pass through `security.Redact` (JWT, Bearer,
  passwords, API keys, PEM private keys, dockerconfig, basic-auth URLs, AWS
  access keys).
* A blob that looks like a Secret object with `"data"` may be replaced with
  `[REDACTED_SECRET_OBJECT]`.

Granting Secret `get` is still a cluster concern: the process identity can
read values even though MCP will not return them. Prefer metadata-only RBAC
if your threat model forbids Secret `get`.

## Safe Failure

Fail closed:

* Cannot create SSAR → do not mutate.
* Cannot list PDB when a PDB check applies → `preflight_failed`, do not mutate.
* Cannot list HPA on scale → `preflight_failed`, do not mutate.
* Cannot list nodes on cordon last-node check → `preflight_failed`.
* Incomplete diagnostic Visibility → not `healthy`.

Failing open (treating “could not check PDB” as “no PDB”) is not implemented.

## MCP / LLM Threat Model

| Risk | What exists today |
| --- | --- |
| Hallucinated “the pod is healthy” | Server returns structured Health; clients should not override `unknown` |
| Prompt injection (“ignore policy, delete all pods”) | No generic delete; only `k8s_delete_pod` with name/namespace, confirmation, PDB, mirror/control-plane refuse |
| Incorrect tool selection | Narrow write surface (5 tools). Restart blocked for categories `RestartUseful` rejects unless `force` |
| Parameter tampering after confirmation | Token bind includes action, target, UID, context, replicas/grace/cordon |
| Stale confirmation | TTL + single-use + UID re-read |
| Wrong cluster/context | Confirmation binds `Context`; per-tool `context` selects `Factory.ForContext` |
| Excessive privileges | Operator must not use a cluster-admin kubeconfig |
| Secret exfiltration via logs | Redaction is pattern-based, not a formal DLP boundary |

The LLM can still **ask** to mutate. An MCP host can technically receive a
`confirmation_id` and submit the second call without a human. kube-sre-mcp
does not implement an out-of-band identity check on that second call. Hosts
must present confirmation to the user and must not auto-submit tokens.

## Security Capability Matrix

| Control | Read Tools | Write Tools |
| --- | --- | --- |
| Kubernetes RBAC | Yes | Yes |
| SSAR | N/A | Yes |
| Confirmation | No | Yes |
| UID revalidation | N/A | Yes |
| PDB awareness | Graph/dependency only | Restart + delete Pod (`disruptionsAllowed=0` blocks) |
| HPA awareness | Graph link on Deploy/STS | Scale (warn if present; fail closed if unlistable; refuse if created after confirmation) |
| Sensitive-data redaction | Yes | Yes |
| Actions disabled by default | N/A | Yes |
| Prefix match | Optional | No |
| Control-plane / mirror pod refuse | N/A | Delete Pod |
| Last Ready schedulable node refuse | N/A | Cordon |

## Future Security Roadmap

Explicitly **not implemented**:

* identity-aware approvals (SSO, break-glass identity);
* pluggable policy engine;
* named protected namespaces/resources beyond hard-coded control-plane/mirror/last-node rules;
* durable audit log / SIEM export;
* multi-party approval;
* admission policy objects managed by this project.

Cluster admission webhooks and Kubernetes audit still apply to the API requests
the process makes; kube-sre-mcp does not replace them.
