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
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakedisco "k8s.io/client-go/discovery/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
)

func TestCRDDiscoveryRefreshAndScope(t *testing.T) {
	fake := &fakedisco.FakeDiscovery{Fake: &ktesting.Fake{}}
	fake.Resources = []*metav1.APIResourceList{}
	cache := kube.NewMapperCache(fake, nil)
	if _, err := cache.GVRForKind("Widget"); err == nil {
		t.Fatal("expected unknown Widget")
	}
	fake.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "example.com/v1",
			APIResources: []metav1.APIResource{
				{Name: "widgets", SingularName: "widget", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}},
				{Name: "clusterscopes", Kind: "ClusterScope", Namespaced: false, Verbs: []string{"get", "list"}},
			},
		},
	}
	cache.Reset()
	gvr, err := cache.GVRForKind("Widget")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if gvr.Resource != "widgets" {
		t.Fatalf("%#v", gvr)
	}
	mp, err := cache.MappingForKind("Widget")
	if err != nil {
		t.Fatal(err)
	}
	if mp.Scope.Name() != meta.RESTScopeNameNamespace {
		t.Fatalf("ns scope %s", mp.Scope.Name())
	}
	if !cache.IsNamespaced("Widget") {
		t.Fatal("widget namespaced")
	}
	mp2, err := cache.MappingForKind("ClusterScope")
	if err != nil {
		t.Fatal(err)
	}
	if mp2.Scope.Name() != meta.RESTScopeNameRoot {
		t.Fatalf("cluster scope %s", mp2.Scope.Name())
	}
}
