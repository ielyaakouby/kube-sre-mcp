# Safe Actions

Diagnostic tools (`k8s_diagnose_*`, find/get/list, logs, events, cluster
health, context) never mutate the cluster.

**Safe Actions** are the five mutating MCP tools. They share one engine
(`internal/action`): feature flag, SelfSubjectAccessReview (SSAR), risk /
blast-radius, confirmation, UID revalidation, then a single typed patch or
delete.

There is no generic `kubectl exec`, generic patch, generic delete, or apply
YAML surface. Those are intentionally absent.

## Supported mutation tools

Registered in `internal/mcp/handlers.go`:

| Tool | Kubernetes mutation | Idempotent hint |
| --- | --- | --- |
| `k8s_restart_deployment` | Strategic merge patch: `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` | no |
| `k8s_scale_workload` | Merge patch `spec.replicas` on Deployment, StatefulSet, or ReplicaSet | yes |
| `k8s_delete_pod` | `DELETE` Pod (`grace_period_seconds`, default 30) | no (`DestructiveHint=true`) |
| `k8s_cordon_node` | Patch Node `spec.unschedulable=true` | yes |
| `k8s_uncordon_node` | Patch Node `spec.unschedulable=false` | yes |

Writes require exact `name` (and `namespace` where namespaced). Prefix match
is not applied.

## Common flow

```text
Action requested
      ↓
Actions enabled?
      ↓
SSAR
      ↓
Exact target lookup
      ↓
Risk / blast-radius checks
      ↓
PDB / HPA / ownership checks
      ↓
Confirmation required
      ↓
Re-read target
      ↓
Validate UID / current state
      ↓
Execute
```

Result `status` values used by the engine (do not invent others):

| `status` | Meaning |
| --- | --- |
| `disabled` | `KUBE_SRE_MCP_ACTIONS_ENABLED=false` |
| `forbidden` | SSAR denied (`authorized: false`) |
| `confirmation_required` | Plan stored; retry with `confirmation_id` |
| `confirmation_mismatch` | Token bind failed **or** UID changed at execution |
| `restart_not_recommended` | RCA category is not restart-useful and `force` is false |
| `blocked_by_pdb` | Matching PDB has `disruptionsAllowed=0` |
| `preflight_failed` | Could not verify PDB/HPA/nodes (fail closed) or HPA appeared after confirmation |
| `error` | API error, invalid args, expired/missing token, last-node refuse, control-plane/mirror refuse |
| `success` | Mutation issued |

Expired or missing confirmation tokens currently return `status: error` with
messages `confirmation_expired` or `confirmation_not_found` (not a separate
status string). `confirmation_replay` is defined in code but unused; reuse
after `Take` looks like not found.

## Confirmation

kube-sre-mcp uses a two-step confirmation protocol for mutating actions. The MCP
host/client is responsible for presenting the confirmation request to the user
and must not automatically submit confirmation IDs without user approval.

First call: omit `confirmation_id`. Response includes `confirmation_id`,
`risk`, `blast_radius`, `preconditions`, `authorized`.

Second call: same tool, same target, same parameters, plus `confirmation_id`.
The token is single-use, TTL-bound (default 120s), and bound to action, kind,
name, namespace, UID, kubeconfig context, and parameters (replicas / grace /
cordon).

kube-sre-mcp enforces those server-side confirmation semantics. It cannot
guarantee that a human approved the second call; an autonomous MCP host could
submit the token. Kubernetes RBAC remains the final authorization boundary.
The LLM is not an approver. See [security.md](security.md).

---

## `k8s_restart_deployment`

Parameters: `name`, `namespace` (required), optional `confirmation_id`,
`context`, `reason_category`, `force`.

### Preconditions

* Actions enabled.
* Deployment GET succeeds.
* If `reason_category` is empty, the engine infers a category from Pod waiting
  reasons (`IMAGE_PULL_FAILURE`, `CONFIG_ERROR`, `CRASH_LOOP`).
* `recommendation.RestartUseful` must be true unless `force=true`.

### Kubernetes Authorization

SSAR: `patch` `deployments` group `apps` in the namespace, named object.

### Risk Evaluation

Default `high`. Becomes `critical` when restart is unlikely to fix the
inferred category. Blast radius includes desired/available replicas, strategy,
context.

### Confirmation

Action key `restart_deployment`. Bound to Deployment UID and context.

### PDB/HPA Checks

**PDB:** list PDBs in the namespace; if a selector overlaps the Deployment
selector and `status.disruptionsAllowed == 0` → `blocked_by_pdb`.
List failure → `preflight_failed`.

**HPA:** not checked on restart (HPA does not own rollout-restart).

### UID Revalidation

Re-GET Deployment; UID must match the token. PDB re-checked.

### Execution

Patch `kubectl.kubernetes.io/restartedAt` on the pod template.

### Failure Modes

`disabled`, `forbidden`, `restart_not_recommended`, `blocked_by_pdb`,
`preflight_failed`, `confirmation_required`, `confirmation_mismatch`, `error`.

---

## `k8s_scale_workload`

Parameters: `kind`, `name`, `namespace`, `replicas`, optional
`confirmation_id`, `context`.

Supported kinds: Deployment, StatefulSet, ReplicaSet. Other kinds → `error`.

`replicas` must be `>= 0` and `<= KUBE_SRE_MCP_MAX_REPLICAS` (default 100).

### Preconditions

Target GET succeeds; current replica count recorded.

### Kubernetes Authorization

SSAR: `patch` on `deployments` / `statefulsets` / `replicasets` in `apps`.

### Risk Evaluation

Default `high`. If an HPA targets the object: `critical` and a precondition
warning that the HPA may revert the replica count. Scale is **not** refused
solely because an HPA exists (the caller must still confirm).

### Confirmation

Action key `scale`. Bound to UID, context, and requested replica count.

### PDB/HPA Checks

**PDB:** not evaluated on scale.

**HPA:** list autoscaling/v2 then v1 HPAs. List error → `preflight_failed`.
If an HPA is created **after** confirmation → `preflight_failed` (“HPA created
after confirmation; refusing to scale”).

### UID Revalidation

Re-read UID; must match.

### Execution

Merge patch `{"spec":{"replicas":N}}`.

### Failure Modes

`disabled`, `forbidden`, `preflight_failed`, `confirmation_required`,
`confirmation_mismatch`, `error` (including max replicas).

---

## `k8s_delete_pod`

Parameters: `name`, `namespace`, optional `grace_period_seconds` (handler
default 30), `confirmation_id`, `context`.

### Preconditions

Pod GET succeeds.

Hard refuse (`status: error`, no confirmation):

* control-plane/system Pod (`kube-system` + apiserver/etcd/scheduler labels, or
  `label.kubernetes.io/control-plane`);
* static/mirror Pod (`kubernetes.io/config.mirror` or config.source file/static).

### Kubernetes Authorization

SSAR: `delete` `pods` in the namespace.

### Risk Evaluation

Default `high`. `critical` if no ownerReferences (unmanaged), owner is
DaemonSet or Job, or namespace is `kube-system`.

### Confirmation

Action key `delete_pod`. Bound to Pod UID, context, grace period.

### PDB/HPA Checks

**PDB:** Pod labels vs PDB selectors; `disruptionsAllowed=0` → `blocked_by_pdb`.
List failure → `preflight_failed`.

**HPA:** not applicable.

### UID Revalidation

Re-GET Pod; UID must match (a recreated Pod is a different object). PDB
re-checked.

### Execution

`Delete` with `GracePeriodSeconds`.

### Failure Modes

`disabled`, `forbidden`, `blocked_by_pdb`, `preflight_failed`,
`confirmation_required`, `confirmation_mismatch`, `error`.

---

## `k8s_cordon_node`

Parameters: `name`, optional `confirmation_id`, `context`. Exact node name.

### Preconditions

Node GET succeeds. Refuses (`status: error`) if this would cordon the **last
Ready schedulable** node. Control-plane labeled nodes are allowed but risk
`critical`.

### Kubernetes Authorization

SSAR: `patch` `nodes`.

### Risk Evaluation

`high` for workers; `critical` for control-plane labels
(`node-role.kubernetes.io/control-plane` or `master`). Last-node refuse is
`critical` risk in the error payload.

### Confirmation

Action key `cordon`. Bound to Node UID, context, `cordon=true`.

### PDB/HPA Checks

Not used. Last-node check is the node-safety analog.

### UID Revalidation

Re-GET Node; UID match; last-node check repeated.

### Execution

Patch `spec.unschedulable=true`.

### Failure Modes

`disabled`, `forbidden`, `preflight_failed` (cannot list nodes),
`confirmation_required`, `confirmation_mismatch`, `error`.

---

## `k8s_uncordon_node`

Same SSAR, confirmation (`action=uncordon`, `cordon=false`), and UID
revalidation as cordon. No last-node restriction (scheduling is being
re-enabled). Patch `spec.unschedulable=false`.

---

## Why Restart Is Not Always a Fix

`RestartUseful` returns **false** for:

* `REGISTRY_AUTHENTICATION_FAILURE`
* `IMAGE_NOT_FOUND`
* `IMAGE_PULL_FAILURE`
* `MISSING_SECRET`
* `MISSING_CONFIGMAP`
* `CONFIG_ERROR`
* `FAILED_MOUNT`
* `FAILED_ATTACH_VOLUME`
* `INSUFFICIENT_CPU` / `INSUFFICIENT_MEMORY`
* `TAINT_NOT_TOLERATED`
* `UNSCHEDULABLE`
* `NO_ELIGIBLE_NODE`

Restarting a Deployment does not create registry credentials, a missing
ConfigMap, a schedulable node, or a bound PVC. The engine returns
`restart_not_recommended` unless the caller sets `force=true`.

Prefer diagnose first:

> Diagnose deployment payment-api and tell me whether restart would actually
> help. Do not execute any action.

## What is not a Safe Action

Not implemented (and not “coming soon” as a hidden tool):

* `kubectl exec` / attach / port-forward
* generic `patch` / `update` / `apply` of arbitrary YAML
* generic delete of Deployments, namespaces, or CRDs
* drain / drain-evict (cordon only)
* rollout undo, image tag changes, env edits

Admission controllers in the cluster still apply to the patches kube-sre-mcp
does send.
