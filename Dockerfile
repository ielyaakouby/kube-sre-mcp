# Copyright 2026 The Kube SRE MCP Authors
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.27.1-bookworm AS build
WORKDIR /src
ARG VERSION=0.1.0-beta.1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN V="${VERSION#v}" && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X kube-sre-mcp/internal/version.Version=${V}" -o /out/kube-sre-mcp ./cmd/kube-sre-mcp

# Optional local distroless image for the stdio MCP binary (not published in v1).
# Kubernetes access still requires a mounted kubeconfig (no in-cluster ServiceAccount auth).
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/kube-sre-mcp /kube-sre-mcp
USER nonroot:nonroot
ENTRYPOINT ["/kube-sre-mcp"]
