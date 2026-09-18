# Native Windows setup

This guide covers **native Windows 10/11** usage of kube-sre-mcp with PowerShell.
Wine is **not** part of the Windows installation procedure. Wine is for
development or cross-platform testing on non-Windows hosts only.

Official Windows artifact:

`kube-sre-mcp-windows-amd64.exe`

That is the recommended binary for modern 64-bit Windows. 32-bit Windows builds
are not official community release artifacts.

## Requirements

* Windows 10 or Windows 11 (amd64 / x64)
* PowerShell 5.1 or later (Windows PowerShell or PowerShell 7+)
* A working kubeconfig that `kubectl` can use
* An MCP host that can launch a local stdio process (Claude Desktop, Cursor, or compatible)

kube-sre-mcp authenticates to Kubernetes with **kubeconfig only**. There is no
in-cluster ServiceAccount fallback.

## kubeconfig on Windows

Default path:

```text
C:\Users\<USERNAME>\.kube\config
```

That is `%USERPROFILE%\.kube\config`.

Set an explicit kubeconfig when the file is not in the default location:

```powershell
$env:KUBECONFIG = "C:\Users\<USERNAME>\.kube\config"
```

Multiple files use the Windows path separator `;`:

```powershell
$env:KUBECONFIG = "C:\Users\<USERNAME>\.kube\config;C:\Users\<USERNAME>\.kube\other"
```

### Flattened kubeconfig and current-context

kube-sre-mcp uses the same loading rules as kubectl:

1. `KUBECONFIG` if set
2. otherwise `%USERPROFILE%\.kube\config`

If `KUBE_SRE_MCP_CONTEXT` is unset, the kubeconfig **current-context** is used.

List and switch contexts with kubectl:

```powershell
kubectl config get-contexts
kubectl config current-context
kubectl config use-context <context-name>
```

For a long-lived MCP process, you can also pin a context:

```powershell
$env:KUBE_SRE_MCP_CONTEXT = "production"
```

### Certificate paths

If the kubeconfig references client certificates, CA files, or exec plugins,
those paths must be valid **Windows** paths (for example `C:\Users\<USERNAME>\.minikube\ca.crt`).
Relative Unix paths and WSL `/home/...` paths will fail in a native Windows process.

Prefer embedding certificates in the kubeconfig (`certificate-authority-data`,
`client-certificate-data`, `client-key-data`) when you need a portable file.

### Multi-cluster usage

Each kubeconfig context is a cluster identity. Per-tool `context` on MCP calls
selects a named context from the same kubeconfig. Mutating confirmation tokens
are bound to that context so a second call cannot silently target another cluster.

## Install the binary

1. Download `kube-sre-mcp-windows-amd64.exe` from the GitHub release
   (`v0.1.0` or later).
2. Optionally verify SHA-256 against `checksums.txt` from the same release.
3. Place it in a directory you control, for example:

```text
C:\Users\<USERNAME>\kube-sre-mcp\kube-sre-mcp-windows-amd64.exe
```

Build from source (Go 1.26.6+):

```powershell
go build -trimpath -ldflags="-s -w -X github.com/ielyaakouby/kube-sre-mcp/internal/version.Version=0.1.0" -o kube-sre-mcp-windows-amd64.exe ./cmd/kube-sre-mcp
```

## Test kubectl first

```powershell
kubectl version --client
kubectl get ns
kubectl cluster-info
```

If kubectl cannot reach the API server, kube-sre-mcp will not either.

## Test kube-sre-mcp directly

```powershell
$env:KUBECONFIG = "C:\Users\<USERNAME>\.kube\config"
$env:KUBE_SRE_MCP_ACTIONS_ENABLED = "false"
.\kube-sre-mcp-windows-amd64.exe --version
.\kube-sre-mcp-windows-amd64.exe --help
```

Starting the binary without `--version` / `--help` speaks MCP on stdio and will
block waiting for a client. Logs are JSON on **stderr**.

Default is disabled. Leave it `false` unless you intend to mutate the cluster
and the kubeconfig identity has matching RBAC.

Writes are available only when `KUBE_SRE_MCP_ACTIONS_ENABLED=true`. They still
run authorization checks, a two-step confirmation flow, and safety guards.

## Claude / MCP configuration

Transport is **stdio only**. JSON config files require **escaped** Windows
backslashes.

Example command path:

```text
C:\\Users\\<USERNAME>\\kube-sre-mcp\\kube-sre-mcp-windows-amd64.exe
```

Example `mcpServers` stanza:

```json
{
  "mcpServers": {
    "kube-sre-mcp": {
      "command": "C:\\Users\\<USERNAME>\\kube-sre-mcp\\kube-sre-mcp-windows-amd64.exe",
      "env": {
        "KUBECONFIG": "C:\\Users\\<USERNAME>\\.kube\\config",
        "KUBE_SRE_MCP_LOG_LEVEL": "info",
        "KUBE_SRE_MCP_ACTIONS_ENABLED": "false"
      }
    }
  }
}
```

Do not put cluster tokens in the MCP config file. Point `KUBECONFIG` at a file
instead.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| Process exits at startup | `KUBECONFIG` or `%USERPROFILE%\.kube\config` exists and has a valid `current-context` |
| kubectl works in WSL but not in PowerShell | You are using a Linux kubeconfig; copy or recreate it for Windows paths |
| Certificate errors | Flatten the kubeconfig or fix `certificate-authority` / client cert paths |
| MCP host cannot start the server | Use an absolute `.exe` path with doubled backslashes in JSON |
| Actions stay disabled | Set `KUBE_SRE_MCP_ACTIONS_ENABLED=true` |
| Wrong cluster | Check `kubectl config current-context` or set `KUBE_SRE_MCP_CONTEXT` |
| 32-bit download missing | Official releases ship **windows-amd64** only |

Further operator notes: [troubleshooting.md](troubleshooting.md).
Security model: [security.md](security.md). Safe actions: [safe-actions.md](safe-actions.md).
