# CrashLoopBackOff demo

A Deployment whose container always exits with status 1.

## Apply

```bash
kubectl apply -f examples/crashloop/manifest.yaml
```

## Expected failure

The Pod enters **CrashLoopBackOff** (`restartCount` increases, waiting reason `CrashLoopBackOff`).

## Example MCP diagnostic question

> Diagnose deployment crashloop-demo in namespace kube-sre-mcp-demo. Why is it in CrashLoopBackOff? Use events, current logs, and previous logs.

## Cleanup

```bash
kubectl delete -f examples/crashloop/manifest.yaml
```
