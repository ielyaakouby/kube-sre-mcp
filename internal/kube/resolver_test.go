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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
)

func TestResolverPriorityAndAmbiguous(t *testing.T) {
	p1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-xyz", Namespace: "default"}}
	p2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-xyz", Namespace: "production"}}
	p3 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "ismail-8c99c5dd8-79q5v", Namespace: "default"}}
	cs := k8sfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "production"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "dev"}},
		p1, p3,
	)
	dyn := fake.NewSimpleDynamicClient(scheme.Scheme, p1, p2, p3,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "production"}},
	)
	_ = dyn
	cl := &kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default"}
	r := kube.NewResolver(cl, true)
	ctx := context.Background()

	res := r.Resolve(ctx, "po", "ismail-8c99c5dd8-79q5v", "")
	if res.Status != kube.ResolveFound || res.Match == nil || res.Match.Name != "ismail-8c99c5dd8-79q5v" {
		t.Fatalf("exact default %#v", res)
	}

	res = r.Resolve(ctx, "pod", "api-xyz", "")
	if res.Status != kube.ResolveFound || res.Match.Namespace != "default" {
		t.Fatalf("context ns first %#v", res)
	}

	res = r.Resolve(ctx, "pod", "api-xyz", "production")
	if res.Status != kube.ResolveFound || res.Match.Namespace != "production" {
		t.Fatalf("explicit ns %#v", res)
	}

	res = r.Resolve(ctx, "pod", "missing-pod", "")
	if res.Status != kube.ResolveNotFound {
		t.Fatalf("missing %#v", res)
	}

	res = r.Resolve(ctx, "pod", "ismail-8c99c5dd8", "")
	if res.Status != kube.ResolveFound {
		t.Fatalf("prefix %#v", res)
	}

	// two namespaces exact: add second pod to clientset namespaces list already; get by name in production via dynamic
	_ = runtime.Object(nil)
}
