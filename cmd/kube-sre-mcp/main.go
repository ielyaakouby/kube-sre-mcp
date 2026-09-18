// Copyright 2026 The Kube SRE MCP Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	mcpserver "github.com/ielyaakouby/kube-sre-mcp/internal/mcp"
	"github.com/ielyaakouby/kube-sre-mcp/internal/observability"
	"github.com/ielyaakouby/kube-sre-mcp/internal/version"
)

func main() {
	fs := flag.NewFlagSet("kube-sre-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	showVersion := fs.Bool("version", false, "Print version and exit")
	showHelp := fs.Bool("help", false, "Print help and exit")
	printUsage := func(w *os.File) {
		fmt.Fprintf(w, `Kube SRE MCP is a Kubernetes diagnostic MCP server.

It speaks the Model Context Protocol over stdio (not HTTP). Configuration is
via environment variables; see docs/configuration.md.

Usage:
  kube-sre-mcp [flags]

Flags:
`)
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(os.Stderr)
	}
	fs.Usage = func() { printUsage(os.Stderr) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if *showHelp {
		printUsage(os.Stdout)
		os.Exit(0)
	}
	if *showVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	cfg := config.Load()
	logger := observability.NewLogger(cfg.LogLevel, os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpserver.Run(ctx, cfg, logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
