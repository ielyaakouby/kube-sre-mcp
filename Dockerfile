# Copyright 2026 The Kube SRE MCP Authors
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.25-bookworm AS build
WORKDIR /src
ARG VERSION=0.1.0-beta.1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN V="${VERSION#v}" && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X kube-sre-mcp/internal/version.Version=${V}" -o /out/kube-sre-mcp ./cmd/kube-sre-mcp

# Distroless image for distributing the stdio MCP binary.
# Kubernetes access still requires a mounted kubeconfig (no in-cluster ServiceAccount auth).
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/kube-sre-mcp /kube-sre-mcp
USER nonroot:nonroot
ENTRYPOINT ["/kube-sre-mcp"]
