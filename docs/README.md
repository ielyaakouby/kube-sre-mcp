# Kube SRE MCP documentation

Kube SRE MCP is a Go MCP server that diagnoses Kubernetes resources through
deterministic analysis of API state. It does not embed an LLM. MCP clients
provide natural-language interaction; this process returns structured evidence,
ranked diagnostic hypotheses, and (when explicitly enabled) a small set of
gated mutating actions.

Supported in v0.1.0: **stdio**. Not supported: HTTP, SSE, streamable HTTP.

## Getting Started

* [Usage](usage.md) — run the server, connect an MCP client, and walk common SRE workflows.
* [Windows](windows.md) — native Windows 10/11 setup (not Wine).
* [Configuration](configuration.md) — environment variables loaded from `internal/config`.
* [Prompt Examples](prompt-examples.md) — copy-paste prompts grouped by skill level.

## Architecture

* [Architecture](architecture.md) — process layout, packages, data flow, and non-goals.
* [Diagnostic Engine](diagnostic-engine.md) — how diagnosis, RCA, confidence, and impact are computed.
* [Resource Graph](resource-graph.md) — ownership, selectors, and bounded dependency walks.

## Security

* [Security Model](security.md) — runtime trust boundaries, confirmation, SSAR, redaction.
* [Safe Actions](safe-actions.md) — mutating tools, preflight checks, and confirmation workflow.
* [RBAC](../deploy/rbac.md) — recommended permissions for the kubeconfig identity.

Vulnerability reporting lives in root [SECURITY.md](../SECURITY.md), not here.

## Operations

* [Troubleshooting](troubleshooting.md) — operators debugging kube-sre-mcp itself.

## Reference

* [CATALOG.md](../CATALOG.md) — every registered MCP tool and its schema.
* [install/README.md](../install/README.md) — build from source.
* [CONTRIBUTING.md](../CONTRIBUTING.md) — development workflow.
* [CHANGELOG.md](../CHANGELOG.md) — release history.
* [v0.1.0 release notes](releases/v0.1.0.md) — first public release.

## Recommended Reading Order

1. [usage.md](usage.md) — how to run and ask useful questions.
2. [architecture.md](architecture.md) — how the process is structured.
3. [diagnostic-engine.md](diagnostic-engine.md) — how diagnostic hypotheses are produced.
4. [resource-graph.md](resource-graph.md) — why related objects are walked.
5. [security.md](security.md) — what the LLM is *not* allowed to authorize.
6. [safe-actions.md](safe-actions.md) — when and how mutations execute.
7. [configuration.md](configuration.md) — production-safe settings.
8. [troubleshooting.md](troubleshooting.md) — when something fails.

Keep [CATALOG.md](../CATALOG.md) nearby as the tool contract. Do not treat README
overviews as a substitute for the Diagnostic Engine or Safe Actions documents.
