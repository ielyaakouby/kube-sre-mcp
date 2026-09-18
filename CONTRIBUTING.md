# Contributing to kube-sre-mcp (Kubernetes SRE MCP)

Thanks for contributing. kube-sre-mcp is a stdio MCP server for Kubernetes diagnostics. Read-only and diagnostic improvements are welcome with ordinary review. Changes to mutating actions, RBAC assumptions, confirmation, and Secret handling are security-sensitive and need extra scrutiny.

The MCP/LLM client is **not** an authorization authority. The Go server must enforce safety independently.

## Licensing of contributions

Unless explicitly stated otherwise, contributions are provided under the Apache License, Version 2.0. See [LICENSE](LICENSE). Community standards: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Development requirements

| Requirement | Notes |
| --- | --- |
| Go | **1.26.6+** (`go 1.26.6` in [go.mod](go.mod)) |
| Docker | Optional. Local image builds only (`make docker-build`, `Dockerfile`). v0.1.0 does not publish images. |
| Kubernetes cluster | **Not required** for unit tests. Tests use fake client-go objects under `internal/*`. A kubeconfig is required to **run** the server against a cluster. |

Module path in Go is `github.com/ielyaakouby/kube-sre-mcp`. After tag `v0.1.0` exists, `go install github.com/ielyaakouby/kube-sre-mcp/cmd/kube-sre-mcp@v0.1.0` is valid.

## Build

Commands match the [Makefile](Makefile):

```bash
make help
make build          # ./bin/kube-sre-mcp
make run            # go run ./cmd/kube-sre-mcp (stdio; needs kubeconfig)
```

Equivalent without Make:

```bash
go build -o bin/kube-sre-mcp ./cmd/kube-sre-mcp
go run ./cmd/kube-sre-mcp
```

## Validation

```bash
make fmt            # gofmt -w
make check          # gofmt check + go vet + go test ./...
make test-race      # go test -race ./...
make license-check  # Apache-2.0 LICENSE text + SPDX on expected files
```

Direct equivalents:

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -l cmd internal
```

CI (`.github/workflows/ci.yml`) runs lint (`gofmt` + `go vet`), tests (including `-race`), a local binary build, `govulncheck ./...`, a non-publishing Docker image build, and license checks on pull requests and on pushes to `main`. Gosec and CodeQL run in dedicated workflows. There is no Docker/GHCR publishing workflow in v1.

Do not add `fmt.Println` / stdout logging on the MCP runtime path. JSON logs belong on **stderr** so stdout stays MCP.

Do not commit `bin/`, kubeconfig files, tokens, or Secret material.

## Project structure

| Path | Role |
| --- | --- |
| `internal/mcp` | MCP registration, handlers, Secret stripping, timeouts |
| `internal/diagnostic` | Kind-specific analyzers and diagnostic pipeline |
| `internal/graph` | Resource graph (owners, selectors, dependencies) |
| `internal/correlation` | RCA hypotheses and evidence ranking |
| `internal/action` | Safe Action Engine (writes; disabled by default) |
| `internal/security` | Redaction and Secret metadata-only summaries |
| `internal/kube` | client-go factory, resolver, error classification |

Full layout: [docs/architecture.md](docs/architecture.md). Tool contract: [CATALOG.md](CATALOG.md).

## Adding or modifying MCP tools

MCP tools are a **public contract**. Clients and prompts depend on names, required fields, and annotations.

Before adding a tool, decide whether the behavior belongs on an **existing** tool (`k8s_diagnose_resource`, `k8s_get_resource`, `k8s_cluster_health`, …). **Discourage tool sprawl.** Eighteen tools is already a large surface; reuse first.

If a tool change is justified:

1. **Define or update the schema** — argument structs and `jsonschema` tags in `internal/mcp/handlers.go` (for example `diagnoseArgs`, `restartArgs`).
2. **Register the tool** — `Server.Register` in `internal/mcp/handlers.go` via `mcp.AddTool`, with `ro()` or `writeAnn(...)`.
3. **Implement or reuse deterministic backend logic** — diagnostic packages, graph, correlation, or `internal/action`. Do not shell out to `kubectl`.
4. **Preserve read-only / write annotations** — `ReadOnlyHint` on diagnostics; write tools set `ReadOnlyHint=false` and `DestructiveHint` / `IdempotentHint` correctly.
5. **Add protocol and contract tests** — `internal/mcp/contract_test.go` (required fields, tool count/names), `internal/mcp/protocol_test.go` (in-memory MCP session behavior), `internal/mcp/register_test.go` (registration / no duplicates).
6. **Update [CATALOG.md](CATALOG.md)** in the same PR.
7. **Update docs or examples** if operators or MCP hosts need new usage (`docs/usage.md`, `docs/prompt-examples.md`, README tool table).
8. **Verify backward compatibility** — renaming or dropping a tool, required field, or annotation is a breaking contract change.

Do not document tools that `Register` does not expose.

## Mutating actions / `internal/action`

Changes under `internal/action/**` are **security-sensitive**.

A new mutating action must **not** be a generic arbitrary Kubernetes operation. Do **not** add unrestricted primitives such as:

* generic `kubectl`
* arbitrary command execution
* arbitrary `patch`
* arbitrary YAML apply
* unrestricted delete
* exec into arbitrary Pods

unless the project explicitly redesigns its threat model.

Every mutation must consider:

* actions **disabled by default** (`KUBE_SRE_MCP_ACTIONS_ENABLED`)
* exact resource identity (writes must not use prefix match)
* cluster / kubeconfig **context binding**
* Kubernetes **SelfSubjectAccessReview (SSAR)**
* least privilege (see [deploy/rbac.md](deploy/rbac.md))
* risk classification
* confirmation, confirmation **parameter binding**, TTL, and **single use**
* UID revalidation before execute
* PDB implications
* HPA implications
* ownership
* blast radius
* **fail-closed** behavior (errors deny the write)
* races / state change between confirmation and execution
* tests (including negative paths)

Details: [docs/safe-actions.md](docs/safe-actions.md), [docs/security.md](docs/security.md).

> The MCP/LLM client is not an authorization authority.

## Test expectations

Prefer behavioral tests with fake client-go objects. There is no required coverage percentage.

| Area | Expect |
| --- | --- |
| `internal/correlation` | Deterministic / evidence tests (ranking, categories, not guessing) |
| `internal/graph` | Owner and dependency tests (UID ownership, selectors, caps) |
| `internal/action` | **Negative and security tests** (disabled flag, SSAR deny, confirmation mismatch, UID change, PDB/HPA/last-node refusals) |
| `internal/security` | Redaction tests (tokens, PEM, Secret `.data` never returned) |
| MCP tools | Contract and protocol tests as above |

Live-cluster tests are optional and must not be required to merge ordinary changes.

## Documentation expectations

**Code is the source of truth.** Docs must not claim unimplemented capabilities.

| If you change | Also update |
| --- | --- |
| MCP tool name, schema, or annotations | [CATALOG.md](CATALOG.md) (and README tool table if listed) |
| Environment variables / defaults | [docs/configuration.md](docs/configuration.md) |
| Diagnostic / RCA behavior | [docs/diagnostic-engine.md](docs/diagnostic-engine.md) |
| Security or action behavior | [docs/security.md](docs/security.md) and/or [docs/safe-actions.md](docs/safe-actions.md) |
| Process layout, transports, auth mode | [docs/architecture.md](docs/architecture.md) |

## Pull requests

1. Keep changes focused. Do not mix unrelated refactors with bug fixes.
2. Target **`main`**.
3. Use the pull request template. Complete the mutating-action checklist if you touch `internal/action/**`.
4. Open an issue first for new mutating tools or large contract changes.

Questions that are not security reports belong in GitHub issues. Vulnerabilities: [SECURITY.md](SECURITY.md) (private reporting only).
