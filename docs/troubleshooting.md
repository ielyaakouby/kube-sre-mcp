# Troubleshooting kube-sre-mcp

This page is for operators debugging **kube-sre-mcp itself**, not for
application incidents inside the cluster. For incident RCA, use
[usage.md](usage.md) and [diagnostic-engine.md](diagnostic-engine.md).

The binary has only `--help` / `--version`. All other behavior is environment
variables ([configuration.md](configuration.md)). Logs are JSON on **stderr**.

## MCP Server Does Not Start

Startup (`cmd/kube-sre-mcp` → `internal/mcp.Run`) loads a Kubernetes client
before serving MCP.

Check:

1. `KUBECONFIG` points at a readable kubeconfig, **or** `~/.kube/config` exists.
2. `KUBE_SRE_MCP_CONTEXT` names a context that exists in that kubeconfig
   (empty = current context).
3. The process can reach the API server (`KUBE_SRE_MCP_API_TIMEOUT_SECONDS`).
4. `KUBE_SRE_MCP_METRICS_LISTEN` is unset **or** the bind address is free.
   A failed metrics bind aborts startup.

Typical stderr: `failed to initialize Kubernetes client: no usable kubeconfig found`.

```bash
export KUBECONFIG="$HOME/.kube/config"
./bin/kube-sre-mcp --version
KUBE_SRE_MCP_LOG_LEVEL=debug ./bin/kube-sre-mcp
```

The process speaks MCP on stdin/stdout. Running it in a TTY without an MCP
host looks “hung”; that is expected.

## Kubernetes Authentication Failure

kubeconfig load order (`internal/kube.Factory.ForContext`):

1. Explicit `KUBECONFIG` if set (including multiple paths)
2. Default local kubeconfig (`~/.kube/config`)

There is no ServiceAccount fallback. Missing kubeconfig or an unknown
`KUBE_SRE_MCP_CONTEXT` fails startup (or that tool call) with a clear error.

Expired tokens, missing files, and unreachable API servers all fail this path.
Fix credentials; kube-sre-mcp does not implement a separate login tool.

## Kubernetes Authorization / 403

Limited RBAC is a supported mode, not a crash.

* Diagnose target GET forbidden → `status: error`, Health `unknown`,
  `permission_denied`.
* Secondary lists (Events, logs, child Pods, graph) fail → Visibility
  `limited`, response `partial`, Health not promoted to `healthy` without
  evidence.
* Writes → `forbidden` after SSAR deny, or API 403 on the patch/delete.

Grant verbs from [deploy/rbac.md](../deploy/rbac.md). Do not treat missing list
permissions as a healthy cluster.

## MCP Client Cannot Connect

* Transport is **stdio only**. HTTP, SSE, and Streamable HTTP MCP are not
  implemented.
* Client `command` must be an **absolute path** to the binary.
* Do not redirect stdout. JSON logs belong on stderr (the server already
  writes them there).
* If you run a locally built image, use `docker run -i` (stdin attached). There is no MCP port to curl. v1 does not publish an official image.

See the `mcpServers` example in the root [README.md](../README.md).

## Tool Not Found

The server registers **18** tools (names `k8s_*`). If the host cannot see them:

* the process failed before `Register`;
* the host cached an old server;
* the host is connected to a different MCP server.

Canonical list: [CATALOG.md](../CATALOG.md). There is no `kubectl` tool.

## Resource Not Found

Resolver status `not_found`. Causes:

* wrong namespace;
* wrong kind alias;
* prefix match off (`KUBE_SRE_MCP_PREFIX_MATCH=false`) and incomplete name;
* object deleted.

Use `k8s_find_resource` with `all_namespaces` or `k8s_get_context` to confirm
the default namespace.

## Ambiguous Resource

Resolver status `ambiguous`. Prefix or cross-namespace search matched more than
one object. The server **does not guess**. Re-run with explicit `namespace`
and the full `name`.

## Prefix Resolution

Default `KUBE_SRE_MCP_PREFIX_MATCH=true` on **read** paths only.

* Diagnostic responses may include `resolution: prefix` and demote `confirmed`
  confidence to `high`.
* Writes ignore prefix match; the name must be exact.

Disable prefix match in production if short names collide often.

## Metrics API Unavailable

`metrics.k8s.io` is optional. Missing metrics-server is **not** an error.

Diagnosis sets `metrics_available: false` (or omits usage). Cluster health
does not require metrics. Do not install metrics-server solely for kube-sre-mcp
unless you want supporting CPU/memory evidence.

## Events Cannot Be Read

Event list failures become a warning Signal (`E-EVT-ERR`, `list_failed`) on
diagnose, or `status: error` on `k8s_get_events`. Grant `get`/`list` on
`events`. Node events use cluster-scoped listing.

## Logs Cannot Be Read

`k8s_get_logs` returns `error` on the log payload (redacted). Diagnose still
completes without log Signals. Grant `get` on `pods/log`. Previous logs need
a container that actually restarted.

Handler caps: default tail 100, max 500. Diagnostic engine uses
`KUBE_SRE_MCP_MAX_LOG_LINES` (default 200, further capped to 80 on the
diagnose path).

## Actions Disabled

Mutating tools return:

```json
{ "status": "disabled", "message": "write actions are disabled (KUBE_SRE_MCP_ACTIONS_ENABLED=false)" }
```

Enable only with matching operator RBAC on the kubeconfig identity.

`K8S_MCP_ACTIONS_ENABLED=true` does **not** enable actions. That name is not
supported. Use `KUBE_SRE_MCP_ACTIONS_ENABLED=true`.

## confirmation_required

Expected first response of a write. Inspect `risk`, `blast_radius`,
`preconditions`. Retry the **same** tool arguments plus `confirmation_id`.

TTL: `KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS` (default 120). Tokens are
in-memory; restarting the process drops them. The MCP host must present this
step to the user and must not auto-submit `confirmation_id`.

## confirmation_mismatch

Token does not match action/target/UID/context/parameters, **or** the object
UID changed between plan and execute. Start the action again (new
confirmation). Do not reuse the id.

## blocked_by_pdb

A matching PodDisruptionBudget has `disruptionsAllowed=0`. Restart Deployment
and delete Pod honor this. Scale and cordon do not use this status.

## HPA Prevents/Restricts Scale

An existing HPA does **not** block scale; it raises risk to `critical` and
warns that replicas may be reverted. Scale **is** refused when:

* HPA objects cannot be listed (`preflight_failed`);
* an HPA appears after confirmation (`preflight_failed`).

## Wrong Kubernetes Context

Every tool accepts optional `context`. The process default is
`KUBE_SRE_MCP_CONTEXT` or the kubeconfig current context.

`k8s_get_context` / `k8s_list_contexts` show what the server will use.
Confirmation tokens bind context; mixing clusters between confirm calls fails
bind match.

## Cluster Health UNKNOWN

Inventory list 403/timeout → that slice is unknown; overall Health uses
`health.Combine` (critical > degraded > unknown > healthy). Missing node or
pod list is incomplete Visibility, not a green cluster.

Deep diagnosis of unhealthy candidates is capped
(`KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES`). `deep_diagnosis_truncated` means
some problems were listed but not fully RCA’d.

## CRD Cannot Be Resolved

RESTMapper/discovery must know the kind. If the CRD is not installed, get/list
fails. The recommended read-only kubeconfig identity can `get`/`list`
**CustomResourceDefinition** objects but **not** arbitrary CRD instances.
Grant those resources explicitly.

Generic diagnosis only reads `status.conditions` (`diagnostic_depth: generic`).
Empty conditions → Health `unknown`, not a domain-specific RCA.

## Prometheus Metrics Endpoint

If `KUBE_SRE_MCP_METRICS_LISTEN` is set, the
process binds an **unauthenticated** Prometheus handler. That is not MCP.

Cannot bind → process does not start. Prefer `127.0.0.1:9090`. Protect the
bind at the network layer if it is not localhost.

## Debugging

| Knob | Default | Use |
| --- | --- | --- |
| `KUBE_SRE_MCP_LOG_LEVEL` | `info` | `debug` for JSON tool traces (`mcp_tool`, request_id, duration_ms, health, result) |
| `--version` | — | Confirm the binary you launched |
| `--help` | — | Confirm you are not looking at a different CLI |

There is no `--kubeconfig` flag; use `KUBECONFIG`. There is no verbose kubectl
trace flag.

Useful checks:

```bash
make test
KUBE_SRE_MCP_LOG_LEVEL=debug ./bin/kube-sre-mcp
```

Unit tests use fake client-go objects and do not need a cluster.
