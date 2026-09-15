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

package graph_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kube-sre-mcp/internal/graph"
)

func TestMatchExpressions(t *testing.T) {
	sel := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"web"}},
	}}
	if !graph.MatchLabelSelector(sel, map[string]string{"app": "web"}) {
		t.Fatal("in")
	}
	if graph.MatchLabelSelector(sel, map[string]string{"app": "db"}) {
		t.Fatal("not in")
	}
}

func BenchmarkMatchLabelSelector(b *testing.B) {
	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web", "tier": "front"}}
	labels := map[string]string{"app": "web", "tier": "front", "pod": "x"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = graph.MatchLabelSelector(sel, labels)
	}
}
