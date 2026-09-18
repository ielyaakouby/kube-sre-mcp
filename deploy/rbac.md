# Recommended Kubernetes RBAC for kube-sre-mcp

```text
MCP client
   →  kube-sre-mcp (stdio)
   →  kubeconfig identity
   →  Kubernetes API server
   →  Kubernetes RBAC (final authorization boundary)
```

kube-sre-mcp does not grant Kubernetes permissions. The process uses the
identity from the selected kubeconfig context. If that identity cannot
`kubectl get secrets`, kube-sre-mcp cannot retrieve those secrets either.

SelfSubjectAccessReview (SSAR) is an extra check on **writes**; a
`cluster-admin` kubeconfig can still mutate if writes are enabled.
**cluster-admin is not required.**

Do not grant broader verbs than the tools you intend to use.

Runtime model: [docs/security.md](../docs/security.md).
Mutations: [docs/safe-actions.md](../docs/safe-actions.md).
Actions stay **disabled** until `KUBE_SRE_MCP_ACTIONS_ENABLED=true`. RBAC
alone does not turn writes on.

## READ_ONLY_DIAGNOSTIC (default)

Use a kubeconfig user that can `get`/`list`/`watch` diagnostic resources,
`create` SelfSubjectAccessReview, metrics API get/list, CRD *definition*
get/list, and Secret `get`/`list` if you want existence/key checks.

Recommended YAML for a **read-only diagnostic** identity: [rbac-readonly.yaml](rbac-readonly.yaml).
That file has no mutating workload, Pod, or Node verbs. SelfSubjectAccessReview
`create` is included so write tools fail closed if actions are later enabled
without write RBAC. Do not mix write-action RBAC into it.

Keep `KUBE_SRE_MCP_ACTIONS_ENABLED=false` for this profile.

### Why Secrets may require metadata/read access

Pod diagnosis checks whether referenced Secrets **exist** and which **keys**
are present (CreateContainerConfigError / imagePullSecrets). Ingress diagnosis
checks TLS Secret existence. MCP handlers **never return Secret `.data`**.

If your threat model forbids Secret `get`, omit that permission and accept
`SecretUnknown` / limited Visibility on those checks.

### Why SSAR is useful on a read-only identity

`create` on `selfsubjectaccessreviews` lets write tools fail closed with
`forbidden` if someone later enables actions without write RBAC. Read tools
do not call SSAR.

### Metrics API

`metrics.k8s.io` `pods` and `nodes` `get`/`list` are optional supporting
evidence. Missing metrics-server is fine; diagnosis continues without them.

| API group | Resources | Verbs |
| --- | --- | --- |
| `""` | pods, pods/log, services, endpoints, namespaces, nodes, events, configmaps, persistentvolumeclaims, persistentvolumes, serviceaccounts, replicationcontrollers | get, list, watch |
| `""` | secrets | get, list |
| `apps` | deployments, replicasets, statefulsets, daemonsets | get, list, watch |
| `batch` | jobs, cronjobs | get, list, watch |
| `networking.k8s.io` | ingresses, ingressclasses, networkpolicies | get, list, watch |
| `discovery.k8s.io` | endpointslices | get, list, watch |
| `storage.k8s.io` | storageclasses | get, list, watch |
| `autoscaling` | horizontalpodautoscalers | get, list, watch |
| `policy` | poddisruptionbudgets | get, list, watch |
| `metrics.k8s.io` | pods, nodes | get, list |
| `authorization.k8s.io` | selfsubjectaccessreviews | create |
| `apiextensions.k8s.io` | customresourcedefinitions | get, list |

CRD **instance** get/list uses the dynamic client; grant those namespaced or
cluster resources separately if you diagnose custom kinds.

## OPERATOR (writes)

In addition to the read profile, enable `KUBE_SRE_MCP_ACTIONS_ENABLED=true`
only when the kubeconfig identity also has:

| API group | Resources | Verbs | Tools |
| --- | --- | --- | --- |
| `apps` | deployments, statefulsets, replicasets | get, list, watch, **patch** | `k8s_restart_deployment`, `k8s_scale_workload` |
| `""` | pods | get, list, watch, **delete** | `k8s_delete_pod` |
| `""` | nodes | get, list, watch, **patch** | `k8s_cordon_node`, `k8s_uncordon_node` |

Still not cluster-admin. Write tools still run SSAR, confirmation, and
PDB/HPA/last-node guards. See [docs/safe-actions.md](../docs/safe-actions.md).

Enabling `KUBE_SRE_MCP_ACTIONS_ENABLED` without write RBAC yields
`forbidden`, not a successful mutation.

Do not point the MCP host at a `cluster-admin` kubeconfig unless you
intentionally want that blast radius.
