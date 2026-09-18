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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ielyaakouby/kube-sre-mcp/internal/graph"
)

func TestSelectorsAndGraphLimits(t *testing.T) {
	if !graph.MatchLabels(map[string]string{"a": "b"}, map[string]string{"a": "b", "c": "d"}) {
		t.Fatal("match")
	}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns", UID: "1"}, Spec: corev1.PodSpec{NodeName: "n", ServiceAccountName: "sa", Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "pvc"}}}}}}
	g := graph.New(graph.PodRef(p))
	b := graph.Builder{}
	bFrom := &graph.Builder{}
	_ = b
	_ = bFrom
	if len(g.Nodes) != 1 {
		t.Fatal(g.Nodes)
	}
}
