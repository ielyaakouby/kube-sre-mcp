# Summary

<!-- What does this PR change? -->

# Why

<!-- What problem does this solve for operators or contributors? -->

# Changes

<!-- Bullet list of notable code/doc changes. -->

# Testing

<!-- Commands you ran and any cluster/MCP scenario. Fake client-go tests are preferred. -->

# Kubernetes / MCP Impact

<!-- Tool names, schema/CATALOG.md, read vs write path, kubeconfig/RBAC impact. "None" is fine. -->

# Security Impact

<!-- Redaction, SSAR, confirmation, Secret handling, logging. "None" is fine. -->

# Documentation

<!-- Which docs were updated, or why none were needed. -->

# Checklist

- [ ] `go test ./...`
- [ ] `go test -race ./...` where applicable
- [ ] `go vet ./...`
- [ ] `gofmt` clean
- [ ] documentation updated
- [ ] `CATALOG.md` updated if MCP tools changed
- [ ] no secrets/private credentials added
- [ ] no stdout logging added to MCP runtime
- [ ] write/action changes preserve SSAR
- [ ] write/action changes preserve confirmation and safety checks
- [ ] action defaults remain safe
- [ ] new behavior is tested
- [ ] backward compatibility considered

## Mutating actions (`internal/action/**`)

Complete this section if the PR touches `internal/action/**` or any mutating MCP tool.

Any mutating behavior must review:

- [ ] exact resource targeting (no prefix match on writes)
- [ ] Kubernetes authorization (SSAR; kubeconfig identity is the authority)
- [ ] confirmation (parameter binding, TTL, single use)
- [ ] UID revalidation before execute
- [ ] PDB implications
- [ ] HPA implications
- [ ] context/cluster binding
- [ ] fail-closed behavior

The MCP/LLM client is **not** an authorization authority. The Go server must enforce safety independently.
