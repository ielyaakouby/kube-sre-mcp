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

package health_test

import (
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/health"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func TestEvaluateRBACUnknown(t *testing.T) {
	vis := &model.Visibility{Limited: true, MissingPermissions: []model.Permission{{Verb: "list", Resource: "pods"}}}
	h := health.Evaluate(nil, vis)
	if h != model.HealthUnknown {
		t.Fatalf("got %s", h)
	}
	sigs := []signal.DiagnosticSignal{signal.New("1", signal.SourceStatus, "c", "ImagePullBackOff", model.SeverityCritical, model.ConfidenceConfirmed, model.ResourceRef{Kind: "Pod", Name: "p"}, "fail")}
	if health.Evaluate(sigs, vis) != model.HealthCritical {
		t.Fatal("critical still wins")
	}
}

func TestFromCounts(t *testing.T) {
	if health.FromCounts(3, 3, false) != model.HealthHealthy {
		t.Fatal("healthy")
	}
	if health.FromCounts(3, 1, false) != model.HealthDegraded {
		t.Fatal("degraded")
	}
	if health.FromCounts(3, 0, false) != model.HealthCritical {
		t.Fatal("critical")
	}
}
