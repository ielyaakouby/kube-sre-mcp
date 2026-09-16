#!/usr/bin/env bash
# Copyright 2026 The Kube SRE MCP Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo ""
echo "======================================"
echo "   Kube SRE MCP — Installation (Go)"
echo "======================================"
echo ""

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go toolchain not installed (need Go 1.26.6+; see go.mod)."
  exit 1
fi
echo "OK  $(go version)"

KUBE="${KUBECONFIG:-$HOME/.kube/config}"
if [ ! -f "$KUBE" ]; then
  echo "WARN: No kubeconfig at $KUBE. kube-sre-mcp requires a kubeconfig to start."
else
  echo "OK  kubeconfig at $KUBE"
fi

cd "$REPO_ROOT"
echo "Downloading modules..."
go mod download
echo "Building Kube SRE MCP..."
go build -ldflags="-s -w -X kube-sre-mcp/internal/version.Version=0.1.0-beta.1" -o bin/kube-sre-mcp ./cmd/kube-sre-mcp
echo "OK  binary at $REPO_ROOT/bin/kube-sre-mcp"

if [[ "${RUN_TESTS:-}" == "1" ]]; then
  go test ./...
fi

echo ""
echo "Run: $REPO_ROOT/bin/kube-sre-mcp"
echo ""
echo "MCP client example:"
echo "  command: $REPO_ROOT/bin/kube-sre-mcp"
echo "  env.KUBECONFIG: ${KUBECONFIG:-$HOME/.kube/config}"
echo "======================================"
