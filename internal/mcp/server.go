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

package mcpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/observability"
	"kube-sre-mcp/internal/version"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	factory := kube.NewFactory(cfg)
	cluster, err := factory.ForContext(cfg.Context)
	if err != nil {
		return err
	}
	metrics := observability.NewMetrics()
	var httpSrv *http.Server
	if cfg.MetricsListen != "" {
		httpSrv, err = observability.StartMetricsHTTP(cfg.MetricsListen, metrics.Handler(), logger)
		if err != nil {
			return err
		}
	}
	impl := &mcp.Implementation{Name: "Kube SRE MCP", Version: version.String()}
	srv := mcp.NewServer(impl, nil)
	s := New(cfg, cluster, factory, metrics)
	s.Logger = logger
	s.Register(srv)
	if logger != nil {
		logger.Info("Kube SRE MCP server starting", "transport", "stdio", "context", cluster.ContextName, "actions", cfg.ActionsEnabled)
	}
	err = srv.Run(ctx, &mcp.StdioTransport{})
	observability.ShutdownMetricsHTTP(httpSrv)
	return err
}
