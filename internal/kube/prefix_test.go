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

package kube_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"kube-sre-mcp/internal/kube"
)

func TestPrefixResolutionTagged(t *testing.T) {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "payment-abc", Namespace: "default"}}
	cs := k8sfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}, p)
	dyn := fake.NewSimpleDynamicClient(scheme.Scheme, p, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	r := kube.NewResolver(&kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default"}, true)
	res := r.Resolve(context.Background(), "Pod", "payment-abc", "default")
	if res.Resolution != "exact" {
		t.Fatalf("exact %#v", res)
	}
	pre := r.Resolve(context.Background(), "Pod", "payment", "default")
	if pre.Status != kube.ResolveFound || pre.Resolution != "prefix" {
		t.Fatalf("prefix %#v", pre)
	}
	exactOnly := kube.NewResolver(&kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default"}, false)
	denied := exactOnly.Resolve(context.Background(), "Pod", "payment", "default")
	if denied.Status == kube.ResolveFound {
		t.Fatalf("writes/exact must not prefix %#v", denied)
	}
}
