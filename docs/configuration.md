# Configuration

All runtime configuration is **environment variables** loaded in
`internal/config.Load`. The binary flags are only `--help` / `-h` and
`--version`.

There is no MCP HTTP, SSE, or streamable HTTP transport in v0.1.0. Stdio MCP is not
configurable.

Never put tokens or passwords in examples or MCP client JSON.
Point at a kubeconfig file.

## Kubernetes

Authentication (`internal/kube.Factory`) is kubeconfig only:

1. Explicit kubeconfig already supported by the project (`KUBECONFIG`, including multiple paths)
2. Default local kubeconfig (`~/.kube/config`)

There is no ServiceAccount / in-cluster fallback. Failure to load kubeconfig
stops the process before MCP starts.

Default namespace comes from the chosen kubeconfig context, then `default`.
When `KUBE_SRE_MCP_CONTEXT` is unset, the kubeconfig `current-context` is used.

| Variable | Default | Description | Example |
| --- | --- | --- | --- |
| `KUBECONFIG` | client-go default (empty in config struct) | Path to kubeconfig. Unset uses `~/.kube/config`. | `/home/you/.kube/config` |
| `KUBE_SRE_MCP_CONTEXT` | current kubeconfig context (empty) | Context name for the process. Per-tool `context` overrides for that call. | `prod-admin` |

## Diagnostics

| Variable | Default | Description | Example |
| --- | --- | --- | --- |
| `KUBE_SRE_MCP_DIAGNOSTIC_TIMEOUT` | `45` | Per-tool `context.WithTimeout` in **seconds**. | `60` |
| `KUBE_SRE_MCP_API_TIMEOUT_SECONDS` | `15` | client-go REST client timeout (seconds). | `20` |
| `KUBE_SRE_MCP_MAX_GRAPH_DEPTH` | `6` | Resource Graph max depth. Excess → `truncated`, `max_depth`. | `8` |
| `KUBE_SRE_MCP_MAX_GRAPH_NODES` | `80` | Resource Graph max nodes. Excess → `truncated`, `max_nodes`. | `120` |
| `KUBE_SRE_MCP_MAX_DEEP_PODS` | `8` | Max child Pods deep-diagnosed under a workload (also caps CronJob jobs). | `12` |
| `KUBE_SRE_MCP_MAX_LOG_LINES` | `200` | Diagnostic log tail cap (diagnose path further caps to 80). MCP `k8s_get_logs` uses 100 default / 500 max, independent of this value. | `300` |
| `KUBE_SRE_MCP_MAX_EVENTS` | `50` | Diagnostic Event cap. MCP `k8s_get_events` uses 30 default / 100 max. | `80` |
| `KUBE_SRE_MCP_MAX_CONCURRENT_K8S` | `8` | Bounded fan-out for deep child diagnosis. | `4` |
| `KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES` | `8` | Cluster-health deep RCA cap. | `4` |
| `KUBE_SRE_MCP_PREFIX_MATCH` | `true` | Prefix resolution on **read** paths. Writes always require exact names. Ambiguous matches are not guessed. | `false` |

## Actions

| Variable | Default | Description | Example |
| --- | --- | --- | --- |
| `KUBE_SRE_MCP_ACTIONS_ENABLED` | `false` | When `true`, mutating tools may execute (still SSAR + confirmation + preflight). When `false`, they return `status: disabled`. | `true` |
| `KUBE_SRE_MCP_MAX_REPLICAS` | `100` | Upper bound for `k8s_scale_workload`. | `50` |
| `KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS` | `120` | Confirmation token TTL (seconds). In-memory, single-use. | `60` |

## Observability

| Variable | Default | Description | Example |
| --- | --- | --- | --- |
| `KUBE_SRE_MCP_LOG_LEVEL` | `info` | `slog` level: `debug`, `info`, `warn`/`warning`, `error`. JSON on stderr. | `debug` |
| `KUBE_SRE_MCP_METRICS_LISTEN` | empty (disabled) | Prometheus HTTP bind. **Unauthenticated.** Failed bind aborts startup. | `127.0.0.1:9090` |

Prometheus series (when the listener is enabled): `mcp_tool_calls_total`,
`mcp_tool_duration_seconds`, `kubernetes_api_errors_total`, `diagnoses_total`,
`diagnosis_root_causes_total`, `actions_total`.

## Flags

| Flag | Default | Description | Example |
| --- | --- | --- | --- |
| `--help` / `-h` | — | Print usage and exit | `kube-sre-mcp --help` |
| `--version` | — | Print version (`v0.1.0`) and exit | `kube-sre-mcp --version` |

## Production-safe defaults

Safe for diagnostic-only production:

* `KUBE_SRE_MCP_ACTIONS_ENABLED=false` (default)
* A read-only kubeconfig identity (see [deploy/rbac.md](../deploy/rbac.md))
* `KUBE_SRE_MCP_METRICS_LISTEN` empty, or `127.0.0.1:9090` only
* `KUBE_SRE_MCP_LOG_LEVEL=info`
* `KUBE_SRE_MCP_PREFIX_MATCH=true` is acceptable; set `false` if short-name
  collisions are common
* Leave graph/timeouts at defaults unless namespaces are huge (then **lower**
  `MAX_DEEP_PODS` / `MAX_CLUSTER_DEEP_DIAGNOSES` / `MAX_CONCURRENT_K8S` to
  protect the API server)

## Dangerous settings

| Setting | Why it is dangerous |
| --- | --- |
| `KUBE_SRE_MCP_ACTIONS_ENABLED=true` | Enables mutations. Require operator RBAC, a host that presents confirmation to the user (do not auto-submit tokens), and a non-admin identity. |
| `KUBE_SRE_MCP_METRICS_LISTEN=0.0.0.0:9090` | Unauthenticated metrics. Bind localhost or firewall it. |
| Very high `MAX_GRAPH_NODES` / `MAX_DEEP_PODS` / `MAX_CONCURRENT_K8S` | API server load and long diagnoses. |
| `KUBE_SRE_MCP_MAX_REPLICAS` far above cluster capacity | Scale tool will allow that replica count if RBAC allows. |
| `KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS` very large | Widens the replay window for a stolen `confirmation_id` (tokens are still single-use). |
| Using a `cluster-admin` kubeconfig | The kubeconfig identity is the authorization boundary. |

Boolean parsing uses Go `strconv.ParseBool` (`1`, `t`, `true`, `TRUE`, etc.).
Invalid integers fall back to the defaults above.
