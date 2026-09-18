# Image-pull demo

A Deployment that references an image that does not exist.

## Apply

```bash
kubectl apply -f examples/image-pull/manifest.yaml
```

## Expected failure

The Pod stays **Pending** / **ImagePullBackOff** (`ErrImagePull` / `ImagePullBackOff` events).

## Example MCP diagnostic question

> Diagnose deployment image-pull-demo in namespace kube-sre-mcp-demo. Why can Kubernetes not pull the image?

## Cleanup

```bash
kubectl delete -f examples/image-pull/manifest.yaml
```
