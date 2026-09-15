# Copyright 2026 The Kube SRE MCP Authors
# SPDX-License-Identifier: Apache-2.0

VERSION ?= 0.1.0-beta.1
IMAGE   ?= kube-sre-mcp
LDFLAGS ?= -s -w -X kube-sre-mcp/internal/version.Version=$(VERSION)

.PHONY: help build build-all build-all-extra run test test-race test-cover fmt fmt-check vet lint check clean docker-build license-check

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  %-16s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build ./bin/kube-sre-mcp
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp ./cmd/kube-sre-mcp

build-all: ## Cross-compile official release binaries into ./bin
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-linux-amd64         ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-linux-arm64         ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-darwin-amd64        ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-darwin-arm64        ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-windows-amd64.exe   ./cmd/kube-sre-mcp

build-all-extra: ## Optional internal targets (not published as official release artifacts)
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux   GOARCH=386   go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-linux-386           ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm   go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-linux-arm           ./cmd/kube-sre-mcp
	CGO_ENABLED=0 GOOS=windows GOARCH=386   go build -trimpath -ldflags="$(LDFLAGS)" -o bin/kube-sre-mcp-windows-386.exe     ./cmd/kube-sre-mcp

run: ## Run the MCP server on stdio (blocks)
	go run -ldflags="$(LDFLAGS)" ./cmd/kube-sre-mcp

test: ## Run unit tests
	go test ./...

test-race: ## Run tests with the race detector
	go test -race ./...

test-cover: ## Run tests with coverage profile (coverage.out)
	go test -coverprofile=coverage.out ./...

fmt: ## Format Go sources with gofmt
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check: ## Fail if gofmt would change files
	@files=$$(gofmt -l .); test -z "$$files" || (echo "gofmt needed:" && echo "$$files" && exit 1)

vet: ## Run go vet
	go vet ./...

lint: fmt-check vet ## Local lint: gofmt check + go vet (no extra toolchain)

check: lint test ## Format check, vet, and tests

license-check: ## Verify LICENSE matches official Apache-2.0 and SPDX markers
	bash scripts/check-license.sh

clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html

docker-build: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .
