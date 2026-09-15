# Installation — Kube SRE MCP

Kube SRE MCP is a Go MCP server (stdio).

## Prerequisites

| Tool | Version | Notes |
| --- | --- | --- |
| Go | 1.25+ | https://go.dev/dl/ |
| kubeconfig | required | `~/.kube/config` or `$KUBECONFIG`. In-cluster ServiceAccount authentication is not supported. |

## Automated

```bash
bash install/install.sh
```

The script verifies Go, downloads modules, and builds `bin/kube-sre-mcp`. Set `RUN_TESTS=1` to also run `go test ./...`.

## Manual

```bash
go mod download
go build -o bin/kube-sre-mcp ./cmd/kube-sre-mcp
./bin/kube-sre-mcp --help
```

## MCP host

Point the MCP client `command` at the built binary. See the root [README.md](../README.md)
and [docs/usage.md](../docs/usage.md). Full documentation index: [docs/README.md](../docs/README.md).

## Kubernetes permissions

kube-sre-mcp uses the identity from your kubeconfig. Recommended verbs:
[deploy/rbac.md](../deploy/rbac.md). In-cluster Helm deployment is not supported
in this release.

## License

Kube SRE MCP is licensed under the Apache License 2.0 (`Apache-2.0`). See the root [LICENSE](../LICENSE).
