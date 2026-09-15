# Changelog

All notable changes to kube-sre-mcp are documented in this file.

The format is inspired by [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0-beta.1] — 2026-09-15

First public community beta. There are no earlier published versions.

### Added

* MCP stdio server implemented in Go (`cmd/kube-sre-mcp`).
* Kubernetes authentication via kubeconfig only (`KUBECONFIG` or `~/.kube/config`).
* Read-only Kubernetes diagnostic tools: resource diagnosis, cluster health, events, logs, find/get/list, and context inspection.
* Bounded resource-graph walks and ranked root-cause hypotheses (RCA) from API evidence.
* Kubernetes native RBAC as the authorization boundary for the kubeconfig identity.
* Secret value protection: Secret `.data` is never returned; logs, events, and errors are redacted.
* Optional guarded mutating actions (restart Deployment, scale workload, delete Pod, cordon/uncordon Node).
* Official binaries for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64).

### Security

* Mutating actions are disabled by default (`KUBE_SRE_MCP_ACTIONS_ENABLED=false`).
* Writes require Kubernetes SelfSubjectAccessReview (SSAR) plus a two-step confirmation token bound to action, target, UID, context, and parameters.
* kube-sre-mcp provides server-side confirmation semantics; the MCP host/client is responsible for presenting confirmation to a user and must not automatically submit confirmation IDs without user approval. Server confirmation is not proof of human interaction.
* Kubernetes RBAC remains the final authorization boundary on the API server.
* Secret values are stripped from MCP responses; action API errors and unknown CRD status maps are redacted or bounded.
* There is no in-cluster ServiceAccount authentication fallback in this release.

[0.1.0-beta.1]: https://github.com/ielyaakouby/kube-sre-mcp/releases/tag/v0.1.0-beta.1
