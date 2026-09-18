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

func TestLogHTTP403NotRegistry(t *testing.T) {
	sigs := []signal.DiagnosticSignal{
		signal.New("1", signal.SourceLog, "Application", "auth_failure", model.SeverityWarning, model.ConfidenceLow, model.ResourceRef{Kind: "Pod", Name: "p"}, "HTTP 403 forbidden"),
	}
	hyps := correlation.Correlate(sigs, nil)
	if len(hyps) == 0 {
		t.Fatal("expected hyp")
	}
	if hyps[0].Category == "REGISTRY_AUTHENTICATION_FAILURE" {
		t.Fatalf("%#v", hyps[0])
	}
	if hyps[0].Category != "APPLICATION_AUTH_FAILURE" {
		t.Fatalf("got %s", hyps[0].Category)
	}
	if hyps[0].Confidence == model.ConfidenceConfirmed {
		t.Fatal("logs must not be confirmed")
	}
}

func TestDuplicateEventsDoNotInflateIndependence(t *testing.T) {
	oom := signal.New("s", signal.SourceStatus, "Memory", "OOMKilled", model.SeverityCritical, model.ConfidenceConfirmed, model.ResourceRef{Kind: "Pod", Name: "p"}, "OOMKilled")
	var evs []signal.DiagnosticSignal
	evs = append(evs, oom)
	for i := 0; i < 5; i++ {
		evs = append(evs, signal.New("e", signal.SourceEvent, "Other", "BackOff", model.SeverityWarning, model.ConfidenceMedium, model.ResourceRef{Kind: "Pod", Name: "p"}, "Back-off restarting failed container"))
	}
	hyps := correlation.Correlate(evs, nil)
	if hyps[0].Category != "OOM_KILLED" {
		t.Fatalf("oom should rank first %#v", hyps)
	}
}

func TestDeterministicOrder(t *testing.T) {
	sigs := []signal.DiagnosticSignal{
		signal.New("1", signal.SourceStatus, "Probe", "READINESS_PROBE_FAILED", model.SeverityWarning, model.ConfidenceHigh, model.ResourceRef{Kind: "Pod", Name: "p"}, "not ready"),
		signal.New("2", signal.SourceEvent, "Probe", "Unhealthy", model.SeverityWarning, model.ConfidenceMedium, model.ResourceRef{Kind: "Pod", Name: "p"}, "Readiness probe failed"),
	}
	first := correlation.Correlate(sigs, nil)
	for i := 0; i < 100; i++ {
		got := correlation.Correlate(sigs, nil)
		if len(got) != len(first) {
			t.Fatal("len")
		}
		for j := range got {
			if got[j].Category != first[j].Category {
				t.Fatalf("order changed at %d", i)
			}
		}
	}
}

func TestRegistryAuthFromImageEvent(t *testing.T) {
	sigs := []signal.DiagnosticSignal{
		signal.New("1", signal.SourceStatus, "Container", "ImagePullBackOff", model.SeverityCritical, model.ConfidenceConfirmed, model.ResourceRef{Kind: "Pod", Name: "p"}, "unauthorized: authentication required"),
		signal.New("2", signal.SourceEvent, "Image", "ErrImagePull", model.SeverityCritical, model.ConfidenceHigh, model.ResourceRef{Kind: "Pod", Name: "p"}, "unauthorized"),
	}
	hyps := correlation.Correlate(sigs, nil)
	if hyps[0].Category != "REGISTRY_AUTHENTICATION_FAILURE" {
		t.Fatalf("%s", hyps[0].Category)
	}
	if hyps[0].Confidence != model.ConfidenceConfirmed {
		t.Fatalf("conf %s", hyps[0].Confidence)
	}
}
