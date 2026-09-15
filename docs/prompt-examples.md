# Prompt Examples

Copy these into an MCP host after kube-sre-mcp is connected. Prefer natural
language; the host should select tools. Prefer **diagnose** over get/list when
something is broken.

The server is deterministic. It does not run an LLM. It does not mutate the
cluster unless write tools are enabled **and** the MCP host completes the
two-step confirmation protocol after user approval (hosts must not auto-submit
`confirmation_id`).

Confirm the cluster first if you use more than one kubeconfig context:

> Which Kubernetes cluster and namespace are you talking to?

---

## Basic

Inventory and inspection. Tools: `k8s_list_resources`, `k8s_get_resource`,
`k8s_find_resource`, `k8s_get_events`, `k8s_get_logs`, `k8s_get_context`.

* List Pods in namespace `production`.
* List Deployments in `kube-system` with label `k8s-app=kube-dns`.
* Inspect Deployment `payment-api` in `production` (summary only, not full RCA).
* Find pod `payment-api-7d9f` — I don’t know the namespace.
* Where is Deployment `checkout`?
* Get Events for Deployment `payment-api` in `production`.
* Warning Events only, all namespaces, limit 30.
* Events for node `worker-3`.
* Show the last 80 lines from the failing container of pod `checkout-xxxx`.
* Previous logs for pod `checkout-xxxx` — it is crash-looping.
* Get Secret `regcred` in `production` — metadata and key names only, never values.

If a name is ambiguous, list matches and stop. Do not guess.

---

## Diagnostic

Use when the question contains *why*, *failing*, *crash*, *pending*,
*ImagePull*, *OOM*, *no endpoints*, *rollout stuck*. Prefer
`k8s_diagnose_resource` (or `k8s_diagnose_pod` / `k8s_diagnose_deployment` /
`k8s_diagnose_service` / `k8s_diagnose_node`).

### CrashLoopBackOff

> Why is pod `checkout-xxxx` in CrashLoopBackOff? Use current and previous
> logs. Report health, top root cause, confidence, and whether a restart
> would help. Do not mutate anything.

### Pending Pod

> Investigate why pod `migrate-0` is Pending. Distinguish CPU/memory,
> taints, nodeSelector, PVC, and unsupported scheduler constraints.
> Say if the scheduling analysis is approximate.

### Service with no endpoints

> Investigate why service `checkout` has no usable endpoints and trace
> through EndpointSlices and Pods.

### Ingress failure

> Diagnose Ingress `public-web`. Check IngressClass, TLS Secret existence
> (not values), backend Service ports, and ready endpoints.

### StatefulSet / PVC

> Why is StatefulSet `mysql` degraded? Include PVC bind state and StorageClass.
>
> Why is PVC `data-mysql-0` still Pending?

### Image pull / OOM / probes

> This pod is in ImagePullBackOff. Is it auth, missing tag, rate limit, or TLS
> to the registry?
>
> Was container `api` OOMKilled? Compare with the memory limit; do not treat
> metrics as a leak prediction.
>
> Readiness probe failing on `api` — distinguish readiness vs liveness vs startup.

### Jobs / CRDs

> Diagnose Job `migrate-schema`. Did it hit backoff limit?
>
> Diagnose CronJob `nightly-report`.
>
> This is a CRD: diagnose `Widget/foo` in `default`. If you don’t understand
> the domain, say `diagnostic_depth: generic` and do not invent controller logic.

Ask the assistant to report:

* `health` (`healthy` / `degraded` / `critical` / `unknown`)
* top Root Cause Hypothesis category and Confidence
* supporting Evidence (status, Events, logs, dependencies)
* Impact (replicas, endpoints, nodes)
* Recommendations — and **whether a restart would actually help**
* Visibility gaps (403 / timeout) instead of assuming healthy

---

## SRE

Tools: `k8s_cluster_health`, then diagnose the worst objects. Do not mutate.

### Cluster health

> Assess cluster health and prioritize critical incidents. Only show problems.
>
> Cluster health for namespace `production`.
>
> Are any nodes NotReady, cordoned, or under DiskPressure / MemoryPressure?
>
> List failing Deployments, StatefulSets, DaemonSets, and Jobs.

### Evidence-driven RCA

> Diagnose deployment `payment-api` and tell me whether restart would actually
> help. Do not execute any action.
>
> Explain the top root cause with Evidence IDs. If confidence is low because
> only logs matched, say so.

### Multi-resource incident

> Start from Service `payments`: matching Pods are not Ready. Diagnose the
> Service, then the unhealthy Pods, then the Deployment. Correlate one
> incident narrative. Do not restart anything until I say so.
>
> Rollout of `frontend` looks stuck. Diagnose the Deployment and the unhealthy
> Pods. Call out old ReplicaSets vs current.

On-call starter:

> Check cluster health. Then diagnose the worst Deployment or Pod you find.
> Explain the root cause with evidence. Recommend the next action, and do not
> mutate anything until I say so.

---

## Safe Actions

Writes require `KUBE_SRE_MCP_ACTIONS_ENABLED=true`, SSAR, and a second call
with `confirmation_id` after the MCP host obtains user approval (do not auto-submit).
If the flag is false, tools return `disabled`.

Do **not** imply autonomous remediation. Plan, wait, then confirm.

### Restart (only if useful)

> Rollout-restart Deployment `payment-api` in `production` **only if** the
> root cause is something a restart can fix. If this is ImagePullBackOff or
> missing credentials, do **not** restart. If confirmation is required, show
> the plan and wait.

### Scale

> Scale Deployment `web` in `staging` to 3 replicas. Check for an HPA first.
> If confirmation is required, show blast radius and wait. Do not retry with
> `confirmation_id` until I approve.

### Delete Pod

> Delete pod `web-xxxx` in `default` so the controller recreates it. If it has
> no owner, treat it as high risk and ask before confirming. If a PDB blocks
> disruption, stop.

### Node cordon

> Cordon `worker-3` for maintenance. If it is the last Ready schedulable node,
> **do not** cordon it.
>
> Uncordon `worker-3` when maintenance is done. Show confirmation before
> executing.

Tools: `k8s_restart_deployment`, `k8s_scale_workload`, `k8s_delete_pod`,
`k8s_cordon_node`, `k8s_uncordon_node`.

---

## Suggested MCP host instructions

```text
You have kube-sre-mcp. For cluster questions:
1. k8s_get_context if the cluster is unclear.
2. Prefer k8s_diagnose_resource (or diagnose_pod/deployment/service/node)
   when the user asks why something is broken. Do not start with kubectl-style
   list dumps.
3. If namespace is unknown, find/diagnose will search; if the result is
   ambiguous, ask the user — never pick silently.
4. Summarize health, root cause category, confidence, evidence, impact, and
   recommendations.
5. Permission errors mean health is unknown, not healthy.
6. Secret values are never available. Do not ask the tools to print Secret .data.
7. Mutating tools require a two-step confirmation protocol. If confirmation_id is returned,
   explain blast radius and only continue when the user confirms. Do not auto-submit confirmation IDs.
8. Do not recommend restart as the primary fix for image pull auth, missing
   image, missing Secret/ConfigMap, or FailedMount.
9. Do not claim kube-scheduler fidelity; Pending analysis is approximate.
10. Do not run writes unless the user clearly asked to mutate.
```
