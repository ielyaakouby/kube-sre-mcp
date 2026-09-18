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

package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	mcpserver "github.com/ielyaakouby/kube-sre-mcp/internal/mcp"
	"github.com/ielyaakouby/kube-sre-mcp/internal/observability"
)

func TestToolContracts(t *testing.T) {
	s := mcpserver.New(config.Config{}, nil, nil, observability.NewMetrics())
	srv := mcp.NewServer(&mcp.Implementation{Name: "Kube SRE MCP", Version: "test"}, nil)
	s.Register(srv)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "t"}, nil)
	sess, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	list, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	wantRequired := map[string][]string{
		"k8s_diagnose_resource":   {"kind", "name"},
		"k8s_diagnose_pod":        {"name"},
		"k8s_diagnose_deployment": {"name"},
		"k8s_diagnose_service":    {"name"},
		"k8s_diagnose_node":       {"name"},
		"k8s_find_resource":       {"kind", "query"},
		"k8s_get_resource":        {"kind", "name"},
		"k8s_list_resources":      {"kind"},
		"k8s_get_logs":            {"pod"},
		"k8s_get_events":          {},
		"k8s_cluster_health":      {},
		"k8s_get_context":         {},
		"k8s_list_contexts":       {},
		"k8s_restart_deployment":  {"name", "namespace"},
		"k8s_scale_workload":      {"kind", "name", "namespace", "replicas"},
		"k8s_delete_pod":          {"name", "namespace"},
		"k8s_cordon_node":         {"name"},
		"k8s_uncordon_node":       {"name"},
	}
	if len(list.Tools) != 18 {
		t.Fatalf("%d", len(list.Tools))
	}
	for _, tl := range list.Tools {
		req, ok := wantRequired[tl.Name]
		if !ok {
			t.Fatalf("unexpected tool %s", tl.Name)
		}
		raw, _ := json.Marshal(tl.InputSchema)
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s schema %v", tl.Name, err)
		}
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			t.Fatalf("%s no properties", tl.Name)
		}
		var gotReq []string
		if r, ok := schema["required"].([]any); ok {
			for _, x := range r {
				gotReq = append(gotReq, x.(string))
			}
		}
		for _, f := range req {
			if _, ok := props[f]; !ok {
				t.Fatalf("%s missing property %s schema=%s", tl.Name, f, raw)
			}
			if !contains(gotReq, f) && len(req) > 0 {
				t.Fatalf("%s required %s not in %v", tl.Name, f, gotReq)
			}
		}
		for k := range props {
			if k == "additionalProperties" {
				continue
			}
			_ = k
		}
	}
}

func TestDuplicateToolNamesImpossible(t *testing.T) {
	seen := map[string]struct{}{}
	list := mcpSession(t, config.Config{})
	got, err := list.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range got.Tools {
		if _, ok := seen[tl.Name]; ok {
			t.Fatalf("duplicate %s", tl.Name)
		}
		seen[tl.Name] = struct{}{}
	}
	if len(seen) != 18 {
		t.Fatalf("%d", len(seen))
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
