# OOMKilled demo

A Deployment that allocates until the container memory limit is exceeded.

## Apply

```bash
kubectl apply -f examples/oomkilled/manifest.yaml
```

## Expected failure

The container is **OOMKilled** (`reason: OOMKilled`, exit code 137). The Pod may restart and repeat.

## Example MCP diagnostic question

> Diagnose deployment oomkilled-demo in namespace kube-sre-mcp-demo. Was the container OOMKilled? Include events and previous logs.

## Cleanup

```bash
kubectl delete -f examples/oomkilled/manifest.yaml
```
