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

package observability_test

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/ielyaakouby/kube-sre-mcp/internal/observability"
)

func TestMetricsUpdatesAndCardinality(t *testing.T) {
	m := observability.NewMetrics()
	m.ObserveTool("k8s_diagnose_pod", 10*time.Millisecond)
	m.ObserveTool("k8s_diagnose_deployment", 20*time.Millisecond)
	m.Diagnoses.WithLabelValues("healthy").Inc()
	m.Diagnoses.WithLabelValues("critical").Inc()
	m.Actions.WithLabelValues("restart_deployment", "confirmation_required").Inc()
	m.Actions.WithLabelValues("scale", "disabled").Inc()
	m.Actions.WithLabelValues("delete_pod", "success").Inc()
	m.APIErrors.WithLabelValues("Pod").Inc()
	if testutil.ToFloat64(m.ToolCalls.WithLabelValues("k8s_diagnose_pod")) < 1 {
		t.Fatal("pod tool")
	}
	if testutil.ToFloat64(m.ToolCalls.WithLabelValues("k8s_diagnose_deployment")) < 1 {
		t.Fatal("deploy tool should not collapse to diagnose")
	}
	if testutil.ToFloat64(m.Actions.WithLabelValues("restart_deployment", "confirmation_required")) < 1 {
		t.Fatal("action requested")
	}
	if testutil.ToFloat64(m.APIErrors.WithLabelValues("Pod")) < 1 {
		t.Fatal("api error")
	}
	families, err := m.Handler().(http.Handler)
	_ = families
	_ = err
}

func TestMetricsHTTPLifecycle(t *testing.T) {
	m := observability.NewMetrics()
	m.ObserveTool("k8s_get_context", time.Millisecond)
	srv, err := observability.StartMetricsHTTP("127.0.0.1:0", m.Handler(), observability.NewLogger("error", io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	defer observability.ShutdownMetricsHTTP(srv)
	resp, err := http.Get("http://" + srv.Addr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)
	if !strings.Contains(body, "mcp_tool_calls_total") {
		t.Fatalf("metrics body %s", body)
	}
	if strings.Contains(body, "payment-a") || strings.Contains(body, "Bearer") {
		t.Fatal("high cardinality or secret in metrics")
	}
	observability.ShutdownMetricsHTTP(srv)
	_, err = http.Get("http://" + srv.Addr + "/metrics")
	if err == nil {
		t.Fatal("expected shutdown")
	}
}

func TestMetricsBindFailure(t *testing.T) {
	lnSrv, err := observability.StartMetricsHTTP("127.0.0.1:0", http.NotFoundHandler(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observability.ShutdownMetricsHTTP(lnSrv)
	_, err = observability.StartMetricsHTTP(lnSrv.Addr, http.NotFoundHandler(), nil)
	if err == nil {
		t.Fatal("expected bind failure")
	}
}

func TestSlogAttrsNoSecrets(t *testing.T) {
	var buf strings.Builder
	log := observability.NewLogger("info", &buf)
	log.Info("mcp_tool", observability.Attrs("req1", "k8s_get_resource", "ctx", "Secret", "default", time.Millisecond, "", "success")...)
	out := buf.String()
	for _, n := range []string{"request_id", "tool", "cluster_context", "kind", "namespace", "duration_ms", "result"} {
		if !strings.Contains(out, n) {
			t.Fatalf("missing %s in %s", n, out)
		}
	}
	if strings.Contains(out, "eyJ") || strings.Contains(out, "Bearer") || strings.Contains(out, "hunter2") {
		t.Fatalf("secret in logs %s", out)
	}
	if strings.Contains(out, `"name"`) {
		t.Fatal("resource name should not be logged as a high-cardinality field")
	}
	_ = slog.LevelInfo
}
