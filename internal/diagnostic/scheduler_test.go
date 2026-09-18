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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func TestSchedulerAnalysisStructured(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("4Gi")}}}
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers:                []corev1.Container{{Name: "c", Image: "x"}},
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}}},
			Affinity:                  &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	e := eng(n, p)
	r := e.Diagnose(context.Background(), "Pod", "p", "default")
	if r.SchedulerAnalysis == nil || r.SchedulerAnalysis.Completeness != "partial" {
		t.Fatalf("scheduler_analysis %#v", r.SchedulerAnalysis)
	}
	foundAA, foundTS := false, false
	for _, u := range r.SchedulerAnalysis.Unsupported {
		if u == "podAntiAffinity" {
			foundAA = true
		}
		if u == "topologySpreadConstraints" {
			foundTS = true
		}
	}
	if !foundAA || !foundTS {
		t.Fatalf("unsupported %#v", r.SchedulerAnalysis.Unsupported)
	}
	if r.RootCause != nil && r.RootCause.Confidence == model.ConfidenceConfirmed && r.RootCause.Category == "UNSUPPORTED_SCHEDULER_CONSTRAINT" {
		t.Fatal("unsupported must not be confirmed kube-scheduler complete")
	}
}
