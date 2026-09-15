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

package observability

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	ToolCalls    *prometheus.CounterVec
	ToolDuration *prometheus.HistogramVec
	APIErrors    *prometheus.CounterVec
	Diagnoses    *prometheus.CounterVec
	RootCauses   *prometheus.CounterVec
	Actions      *prometheus.CounterVec
	reg          *prometheus.Registry
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		ToolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_tool_calls_total", Help: "MCP tool invocations",
		}, []string{"tool"}),
		ToolDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "mcp_tool_duration_seconds", Help: "MCP tool duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"tool"}),
		APIErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubernetes_api_errors_total", Help: "Kubernetes API errors",
		}, []string{"kind"}),
		Diagnoses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "diagnoses_total", Help: "Diagnoses by health",
		}, []string{"health"}),
		RootCauses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "diagnosis_root_causes_total", Help: "Root cause categories",
		}, []string{"category"}),
		Actions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "actions_total", Help: "Action engine results",
		}, []string{"action", "result"}),
		reg: reg,
	}
	reg.MustRegister(m.ToolCalls, m.ToolDuration, m.APIErrors, m.Diagnoses, m.RootCauses, m.Actions)
	return m
}

func (m *Metrics) ObserveTool(tool string, d time.Duration) {
	if m == nil {
		return
	}
	m.ToolCalls.WithLabelValues(tool).Inc()
	m.ToolDuration.WithLabelValues(tool).Observe(d.Seconds())
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

func StartMetricsHTTP(addr string, h http.Handler, logger *slog.Logger) (*http.Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if logger != nil {
			logger.Error("metrics server bind failed", "addr", addr, "error", err)
		}
		return nil, err
	}
	srv := &http.Server{Addr: ln.Addr().String(), Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			if logger != nil {
				logger.Error("metrics server failed", "addr", addr, "error", err)
			}
		}
	}()
	if logger != nil {
		logger.Info("metrics endpoint listening", "addr", ln.Addr().String(), "note", "unauthenticated; bind to localhost unless network policy/auth is provided by the operator")
	}
	return srv, nil
}

func ShutdownMetricsHTTP(srv *http.Server) {
	if srv == nil {
		return
	}
	shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}
