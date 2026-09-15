# Security

Runtime security architecture (trust boundaries, SSAR, confirmation, redaction):
**[docs/security.md](docs/security.md)**.

Mutating tool behavior: [docs/safe-actions.md](docs/safe-actions.md).
Recommended kubeconfig RBAC: [deploy/rbac.md](deploy/rbac.md).

kube-sre-mcp talks to the Kubernetes API with the credentials you give it.
Treat the process like `kubectl`: anyone who can invoke mutating MCP tools with
writes enabled can change the cluster **within that identity’s RBAC**.

The MCP/LLM client is **not** an authorization authority.

## Supported versions

Security fixes are accepted on `main` of
[github.com/ielyaakouby/kube-sre-mcp](https://github.com/ielyaakouby/kube-sre-mcp).

There is no published LTS matrix yet. If you run a tagged release, include that
version in your report so maintainers can tell whether the issue still applies
to current `main`. Current software version is `0.1.0-beta.1` (`internal/version`).

## Reporting vulnerabilities

**Do not** open a public GitHub issue for security reports.

Use **GitHub Private Vulnerability Reporting** on this repository:

* Security tab: https://github.com/ielyaakouby/kube-sre-mcp/security
* New private advisory (when the setting is enabled): https://github.com/ielyaakouby/kube-sre-mcp/security/advisories/new

Private Vulnerability Reporting is a **GitHub repository setting**. Editing this
file does not enable it. After the public repository exists, a maintainer must
turn on **Settings → Code security → Private vulnerability reporting**.

If that setting is not enabled yet, contact maintainers through a private GitHub
channel. Do not file a public issue as a workaround.

**Do not** attach cluster credentials, kubeconfig files, tokens, Secret `.data`,
or production logs that may contain secrets.

## What to include

* kube-sre-mcp version (`kube-sre-mcp --version`) or git commit
* Whether `KUBE_SRE_MCP_ACTIONS_ENABLED` was true
* MCP tool name and a **redacted** request/response (no Secret values)
* Kubernetes version and whether the identity was `cluster-admin`
* Impact: information disclosure, unintended mutation, confirmation bypass, etc.
* Reproduction against a **throwaway** cluster when possible

## Disclosure expectations

Maintainers will acknowledge reports through the private channel used to send
them. Coordinated disclosure is preferred. Do not post a write-up until a fix
is available or the maintainers have agreed a timeline.

There is no bug-bounty program unless one is announced separately.

## Security-sensitive areas

Reviewers should treat these as high-impact:

* `internal/action` (confirmation bind, SSAR, PDB/HPA/UID checks)
* `internal/security` (redaction; Secret summaries)
* `internal/mcp` Secret get handler
* Recommended kubeconfig RBAC in `deploy/rbac.md`
* Optional unauthenticated Prometheus bind (`KUBE_SRE_MCP_METRICS_LISTEN`)

## Operational notes

* Keep `KUBE_SRE_MCP_ACTIONS_ENABLED=false` unless writes are required.
* Prefer a read-only kubeconfig identity for diagnostic-only use.
* The optional Prometheus listener is unauthenticated; bind to localhost or
  protect it at the network layer.
* Secret `.data` is never returned by MCP handlers; still avoid granting Secret
  `get` if your threat model forbids it.
