# Changelog

All notable changes to kube-sre-mcp are documented in this file.

The format is inspired by [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-09-18

First public release of kube-sre-mcp.

### Added

* MCP server over stdio
* kubeconfig-based Kubernetes authentication
* Kubernetes resource diagnostics
* Pod, Deployment, Service, Node, StatefulSet, DaemonSet, Job, CronJob, Ingress and PVC diagnostics
* Kubernetes Events analysis
* container logs and previous logs analysis
* CrashLoopBackOff detection
* OOMKilled detection
* image-pull diagnostics
* Pending/scheduling diagnostics
* resource relationship graph
* ranked diagnostic hypotheses
* actionable recommendations
* optional guarded remediation actions

### Security

* read-only behavior by default
* write actions disabled unless explicitly enabled
* authorization checks before actions
* two-step confirmation for write operations
* safety guards around disruptive actions
* Secret data is not returned

### Known limitations

* stdio MCP transport only
* kubeconfig authentication only
* no in-cluster ServiceAccount authentication
* evidence-based diagnostic hypotheses, not deterministic causal RCA

[0.1.0]: https://github.com/ielyaakouby/kube-sre-mcp/releases/tag/v0.1.0
