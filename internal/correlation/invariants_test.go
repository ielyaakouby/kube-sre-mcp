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

package correlation_test

import (
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/correlation"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func TestCorrelationInvariants(t *testing.T) {
	ref := model.ResourceRef{Kind: "Pod", Name: "p"}
	status := signal.New("E1", signal.SourceStatus, "Container", "CrashLoopBackOff", model.SeverityCritical, model.ConfidenceConfirmed, ref, "crashloop")
	ready := signal.New("E2", signal.SourceStatus, "Pod", "appears healthy", model.SeverityInfo, model.ConfidenceHigh, ref, "appears healthy")
	hyps := correlation.Correlate([]signal.DiagnosticSignal{status, ready}, nil)
	if len(hyps) == 0 {
		t.Fatal("expected rca")
	}
	for _, h := range hyps {
		if len(h.SupportingEvidenceIDs) == 0 {
			t.Fatalf("rca without evidence %#v", h)
		}
		if h.Category == "CRASH_LOOP" && len(h.ContradictingEvidenceIDs) > 0 && h.Confidence == model.ConfidenceConfirmed {
			t.Fatal("contradiction must demote confirmed")
		}
	}
	onlyLog := signal.New("L", signal.SourceLog, "Application", "auth_failure", model.SeverityWarning, model.ConfidenceLow, ref, "401")
	lh := correlation.Correlate([]signal.DiagnosticSignal{onlyLog}, nil)
	if lh[0].Confidence == model.ConfidenceConfirmed {
		t.Fatal("log-only confirmed")
	}
}

func BenchmarkCorrelate(b *testing.B) {
	ref := model.ResourceRef{Kind: "Pod", Name: "p"}
	sigs := []signal.DiagnosticSignal{
		signal.New("1", signal.SourceStatus, "Container", "CrashLoopBackOff", model.SeverityCritical, model.ConfidenceConfirmed, ref, "crash"),
		signal.New("2", signal.SourceEvent, "Container", "BackOff", model.SeverityWarning, model.ConfidenceHigh, ref, "back-off restarting"),
		signal.New("3", signal.SourceLog, "Application", "panic", model.SeverityCritical, model.ConfidenceLow, ref, "panic"),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = correlation.Correlate(sigs, nil)
	}
}
