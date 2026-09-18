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

package diagnostic_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/ielyaakouby/kube-sre-mcp/internal/diagnostic"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func TestGenericCRDConditions(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": "w", "namespace": "default"},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False", "reason": "ReconcileFailed", "message": "boom"},
			},
		},
	}}
	r := diagnostic.GenericDiagnoseForTest(model.ClusterInfo{Context: "c"}, model.ResourceRef{Kind: "Widget", Name: "w", Namespace: "default"}, obj)
	if r.DiagnosticDepth != "generic" {
		t.Fatalf("depth %s", r.DiagnosticDepth)
	}
	if r.Health == model.HealthHealthy {
		t.Fatal("false ready must not be healthy")
	}
	found := false
	for _, e := range r.Evidence {
		if e.Message != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected condition evidence")
	}
}

func TestGenericCRDNoStatusUnknown(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": "w"},
	}}
	r := diagnostic.GenericDiagnoseForTest(model.ClusterInfo{}, model.ResourceRef{Kind: "Widget", Name: "w"}, obj)
	if r.Health != model.HealthUnknown {
		t.Fatalf("got %s", r.Health)
	}
	if r.DiagnosticDepth != "generic" {
		t.Fatal(r.DiagnosticDepth)
	}
}
