# Kube SRE MCP

[![CI](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/ci.yml)
[![Security](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/security.yml/badge.svg)](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/security.yml)
[![CodeQL](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/codeql.yml/badge.svg)](https://github.com/ielyaakouby/kube-sre-mcp/actions/workflows/codeql.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/ielyaakouby/kube-sre-mcp)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/ielyaakouby/kube-sre-mcp)](https://github.com/ielyaakouby/kube-sre-mcp/releases)

[Why](#why-kube-sre-mcp) | [What it can do](#what-it-can-do) | [Example](#example) | [Safety](#safety) | [Getting Started](#getting-started) | [MCP Client Setup](#mcp-client-setup) | [Documentation](#documentation) | [Contributing](#contributing) | [Security](#security) | [License](#license)

**Give your AI assistant an SRE that understands your Kubernetes cluster.**

Kube SRE MCP is a Kubernetes troubleshooting and SRE diagnostics [MCP](https://modelcontextprotocol.io/) server, built in Go with `client-go`. Its focus is a **deterministic diagnostic engine**: it collects, normalizes, correlates, and ranks Kubernetes evidence such as status, conditions, Events, logs, dependencies, scheduling signals, storage signals, and optional metrics. The result is ranked diagnostic hypotheses with supporting evidence, impact, visibility, confidence, and recommendations.

There is no embedded LLM. The MCP host selects tools and may interpret or narrate the structured output. Evidence collection, normalization, correlation, and hypothesis ranking run deterministically inside `kube-sre-mcp`.

```text
Observe → Diagnose → Explain → Safely Act
```

**v0.1.0 scope**

- Local execution
- stdio transport
- kubeconfig authentication
- Read-only by default
- No HTTP transport
- No in-cluster ServiceAccount authentication
- No official container image
- Kubernetes RBAC remains the authorization boundary

**Diagnostic results are evidence-based hypotheses, not guaranteed causal root-cause analysis.**

## Why Kube SRE MCP

General-purpose Kubernetes MCP servers provide broad Kubernetes access and operational capabilities.

Kube SRE MCP intentionally focuses on a specialized, deterministic **SRE diagnostic layer**.

**The goal is not to replace `kubectl` or provide generic Kubernetes CRUD.**

It is designed to help answer:

- What is unhealthy?
- Why does it appear unhealthy?
- What evidence supports that diagnosis?
- What is impacted?
- How confident is the diagnosis?
- What should be investigated or done next?

**Diagnostics-first, not generic CRUD.**

```text
                    Kubernetes API
                          │
            ┌─────────────┼─────────────┐
            │             │             │
          Status        Events         Logs
            │             │             │
            └─────────────┼─────────────┘
                          │
                   Dependencies
                   Scheduling
                     Storage
                     Metrics
                          │
                          ▼
                 Normalize Signals
                          │
                          ▼
                 Correlate Evidence
                          │
                          ▼
               Rank Diagnostic
                  Hypotheses
                          │
                          ▼
            Confidence + Impact
                 + Visibility
                          │
                          ▼
                  Recommendations
                          │
                          ▼
              Optional Guarded Actions
```

## What it can do

- Diagnose Pods, Deployments, Services, Nodes and other Kubernetes resources
- Correlate status, Events, logs, dependencies and optional metrics
- Produce deterministic, ranked diagnostic hypotheses with supporting evidence
- Report partial visibility and RBAC gaps instead of treating missing evidence as healthy
- Expose impact and recommendations
- Detect common Kubernetes failures such as CrashLoopBackOff, OOMKilled, image pull, scheduling, storage and backend issues
- Inspect cluster health and unhealthy workloads
- Read Events and logs with redaction
- Diagnose generic Kubernetes resources and CRDs through discovery and `status.conditions`
- Support guarded optional actions only when explicitly enabled

See [`CATALOG.md`](CATALOG.md) for the complete MCP tool list, schemas, arguments and RBAC requirements.

## Example

Illustrative (conceptual, not a captured fixture):

```text
User:
Why is deployment payment-api unhealthy?

Diagnosis:
health: critical
root_cause.category: REGISTRY_AUTHENTICATION_FAILURE
root_cause.confidence: high

Evidence:
- Image pull failure
- Registry authentication rejected
- Repeated FailedPull / ErrImagePull events

Recommendation:
Verify the imagePullSecret configuration.
```

## Safety

Read-only by default. Write actions require `KUBE_SRE_MCP_ACTIONS_ENABLED=true`.

Guarded actions run authorization and preflight checks, then a two-step confirmation flow. The MCP host must obtain human approval before submitting the confirmation token. Kubernetes RBAC remains the final authorization boundary.

[Security](docs/security.md) · [Safe actions](docs/safe-actions.md) · [RBAC](deploy/rbac.md) · [Vulnerability reporting](SECURITY.md)

## Getting Started

Go is not required when using a prebuilt [GitHub Release](https://github.com/ielyaakouby/kube-sre-mcp/releases) archive. You need a reachable Kubernetes API, a local kubeconfig, and an MCP client that can spawn a local stdio process.

Uses `KUBECONFIG` if set, otherwise `~/.kube/config`. Optional `KUBE_SRE_MCP_CONTEXT`; otherwise the kubeconfig current-context.

### Linux and macOS

Install into `$HOME/.local/bin` so `sudo` is not required. Choose the archive that matches your OS and CPU. The commands below use `linux_amd64`; substitute `linux_arm64`, `darwin_amd64`, or `darwin_arm64` as needed.

```text
kube-sre-mcp_${VERSION}_linux_amd64.tar.gz
kube-sre-mcp_${VERSION}_linux_arm64.tar.gz
kube-sre-mcp_${VERSION}_darwin_amd64.tar.gz
kube-sre-mcp_${VERSION}_darwin_arm64.tar.gz
kube-sre-mcp_${VERSION}_windows_amd64.zip
```

Verify the SHA256 checksum from `checksums.txt` **before** extracting or installing the binary.

```bash
VERSION=0.1.0
ARCHIVE="kube-sre-mcp_${VERSION}_linux_amd64.tar.gz"
BASE_URL="https://github.com/ielyaakouby/kube-sre-mcp/releases/download/v${VERSION}"

wget "${BASE_URL}/${ARCHIVE}"
wget "${BASE_URL}/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  grep -F "${ARCHIVE}" checksums.txt | sha256sum -c -
else
  grep -F "${ARCHIVE}" checksums.txt | shasum -a 256 -c -
fi

tar -xzf "${ARCHIVE}"

mkdir -p "$HOME/.local/bin"

mv kube-sre-mcp "$HOME/.local/bin/kube-sre-mcp"

chmod +x "$HOME/.local/bin/kube-sre-mcp"
```

Ensure `$HOME/.local/bin` is on `PATH` in the current session:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

To keep that for new terminals, append the same line to the config file for **your** shell (`~/.bashrc` for bash, `~/.zshrc` for zsh)—do not add both unless you use both shells:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
```

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

Validate:

```bash
kube-sre-mcp --version
kube-sre-mcp --help
```

Windows uses a ZIP, not this Unix path. See [docs/windows.md](docs/windows.md).

### Build from source

Go 1.26.6+ (see `go.mod`):

```bash
git clone https://github.com/ielyaakouby/kube-sre-mcp.git
cd kube-sre-mcp
make build
./bin/kube-sre-mcp --help
```

## MCP Client Setup

Claude and other MCP hosts that can spawn a local stdio process can use the same configuration. After the Getting Started install, the Unix binary is `$HOME/.local/bin/kube-sre-mcp` **in a shell**. MCP clients typically do **not** run through a shell and do **not** expand `$HOME`, so `command` must be a real absolute filesystem path.

`KUBE_SRE_MCP_ACTIONS_ENABLED=false` keeps the server read-only. Setting it to `true` enables only the guarded write tools (restart, scale, delete Pod, cordon/uncordon), not generic Kubernetes CRUD. See [Safety](#safety).

Linux:

```json
{
  "mcpServers": {
    "kube-sre-mcp": {
      "command": "/home/USERNAME/.local/bin/kube-sre-mcp",
      "env": {
        "KUBECONFIG": "/home/USERNAME/.kube/config",
        "KUBE_SRE_MCP_ACTIONS_ENABLED": "false"
      }
    }
  }
}
```

macOS:

```json
{
  "mcpServers": {
    "kube-sre-mcp": {
      "command": "/Users/USERNAME/.local/bin/kube-sre-mcp",
      "env": {
        "KUBECONFIG": "/Users/USERNAME/.kube/config",
        "KUBE_SRE_MCP_ACTIONS_ENABLED": "false"
      }
    }
  }
}
```

Replace `USERNAME` with your account name.

### Windows

For Windows installation and MCP client configuration, see [docs/windows.md](docs/windows.md).

## Documentation

- [Documentation index](docs/README.md)
- [MCP tool catalog](CATALOG.md)
- [Architecture](docs/architecture.md)
- [Diagnostic engine](docs/diagnostic-engine.md)
- [Security](docs/security.md)
- [Configuration](docs/configuration.md)
- [Troubleshooting](docs/troubleshooting.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Open a pull request against `main` after `make check`. For substantial changes, [open an issue](https://github.com/ielyaakouby/kube-sre-mcp/issues) first.

## Security

Report vulnerabilities privately. Do not open a public GitHub issue. See [SECURITY.md](SECURITY.md).

## License

Kube SRE MCP is licensed under the [Apache License 2.0](LICENSE).

Copyright 2026 The Kube SRE MCP Authors.

Kube SRE MCP is an independent open-source project and is not a CNCF-hosted project.
