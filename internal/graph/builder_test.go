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
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"kube-sre-mcp/internal/graph"
)

func TestBuilderDeploymentOwnersAndPDB(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "x"}}}}}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "ns", UID: "rs", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns", UID: "p", Labels: map[string]string{"app": "a"}, OwnerReferences: []metav1.OwnerReference{{UID: "rs"}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "x"}}, Volumes: []corev1.Volume{{Name: "proj", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "tok"}}}}}}}}}}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns", Labels: map[string]string{"app": "a"}, OwnerReferences: []metav1.OwnerReference{{UID: "zzz"}}}}
	pdbOK := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "ns"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	pdbBad := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "ns"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "nope"}}}}
	cs := fake.NewSimpleClientset(d, rs, pod, other, pdbOK, pdbBad)
	b := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: 4, MaxNodes: 50}}
	g, err := b.Deployment(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, n := range g.Nodes {
		names[n.Kind+"/"+n.Name] = true
	}
	if !names["Pod/p"] || names["Pod/other"] {
		t.Fatalf("owner filter %#v", names)
	}
	if !names["Secret/tok"] {
		t.Fatal("projected secret")
	}
	if !names["PodDisruptionBudget/ok"] || names["PodDisruptionBudget/bad"] {
		t.Fatalf("pdb match %#v", names)
	}
}

func TestGraphTruncation(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "ns", UID: "rs", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	objs := []runtime.Object{d, rs}
	for i := 0; i < 5; i++ {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: string(rune('a' + i)), Namespace: "ns", UID: types.UID(string(rune('1' + i))), Labels: map[string]string{"app": "a"}, OwnerReferences: []metav1.OwnerReference{{UID: "rs"}}}}
		objs = append(objs, p)
	}
	cs := fake.NewSimpleClientset(objs...)
	b := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: 6, MaxNodes: 3}}
	g, err := b.Deployment(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Truncated || g.TruncationReason == "" {
		t.Fatalf("expected truncation nodes=%d reason=%s", len(g.Nodes), g.TruncationReason)
	}
}

func TestServiceIngressBuilders(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "s"}}}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "i", Namespace: "ns"}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "s"}}}}}}}}}}
	cs := fake.NewSimpleClientset(svc, ing)
	b := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: 3, MaxNodes: 20}}
	sg, err := b.Service(context.Background(), svc)
	if err != nil || sg == nil {
		t.Fatal(err)
	}
	ig, err := b.Ingress(context.Background(), ing)
	if err != nil || len(ig.Edges) == 0 {
		t.Fatalf("%v %#v", err, ig)
	}
}
