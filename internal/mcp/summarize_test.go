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

package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kube-sre-mcp/internal/model"
)

func TestSummarizeUnknownCRDOmitsSensitiveStatus(t *testing.T) {
	secret := "supersecret-crd-token-value"
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":      "w",
			"namespace": "default",
			"labels":    map[string]any{"app": "demo"},
		},
		"status": map[string]any{
			"phase":            "Failed",
			"token":            secret,
			"connectionString": "postgres://user:hunter2@db/app",
			"conditions": []any{
				map[string]any{
					"type":    "Ready",
					"status":  "False",
					"reason":  "AuthFailed",
					"message": "password=should-redact Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
				},
			},
		},
	}}
	sum := summarize("Widget", obj, model.ResourceRef{Kind: "Widget", Name: "w", Namespace: "default"})
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if strings.Contains(out, secret) || strings.Contains(out, "hunter2") || strings.Contains(out, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Fatalf("sensitive CRD status leaked: %s", out)
	}
	m, ok := sum.(map[string]any)
	if !ok {
		t.Fatalf("type %T", sum)
	}
	st, _ := m["status"].(map[string]any)
	if st == nil {
		t.Fatal("expected bounded status summary")
	}
	if _, ok := st["token"]; ok {
		t.Fatal("arbitrary status.token must not be copied")
	}
	if _, ok := st["connectionString"]; ok {
		t.Fatal("arbitrary status.connectionString must not be copied")
	}
	if st["phase"] != "Failed" {
		t.Fatalf("phase: %#v", st["phase"])
	}
}

func TestSummarizePodDoesNotDumpFullStatus(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": "p", "namespace": "ns"},
		"spec":       map[string]any{"nodeName": "n1"},
		"status":     map[string]any{"phase": "Running", "secretDump": "should-not-matter"},
	}}
	sum := summarize("Pod", obj, model.ResourceRef{Kind: "Pod", Name: "p"})
	raw, _ := json.Marshal(sum)
	if strings.Contains(string(raw), "secretDump") {
		t.Fatalf("pod summary dumped extra status: %s", raw)
	}
}
