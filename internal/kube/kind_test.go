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

	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
)

func TestNormalizeKind(t *testing.T) {
	cases := map[string]string{
		"po": "Pod", "pod": "Pod", "pods": "Pod", "deploy": "Deployment",
		"sts": "StatefulSet", "ds": "DaemonSet", "svc": "Service", "ing": "Ingress",
		"cm": "ConfigMap", "pvc": "PersistentVolumeClaim", "pv": "PersistentVolume",
		"ns": "Namespace", "sa": "ServiceAccount", "node": "Node", "Pod": "Pod",
	}
	for in, want := range cases {
		if got := kube.NormalizeKind(in); got != want {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestClusterScoped(t *testing.T) {
	if !kube.IsClusterScopedKind("Node") || kube.IsClusterScopedKind("Pod") {
		t.Fatal("scope")
	}
	gvr, ok := kube.KnownGVR("Deployment")
	if !ok || gvr.Group != "apps" || gvr.Resource != "deployments" {
		t.Fatalf("%v %v", gvr, ok)
	}
}
