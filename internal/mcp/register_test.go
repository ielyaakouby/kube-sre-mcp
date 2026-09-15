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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/observability"
)

func TestRegisterDoesNotPanic(t *testing.T) {
	s := New(config.Config{ActionsEnabled: true}, nil, nil, observability.NewMetrics())
	srv := mcp.NewServer(&mcp.Implementation{Name: "Kube SRE MCP", Version: "test"}, nil)
	s.Register(srv)
	if len(s.tools) != 18 {
		t.Fatalf("tools %d %#v", len(s.tools), s.tools)
	}
	seen := map[string]struct{}{}
	for _, n := range s.tools {
		if _, ok := seen[n]; ok {
			t.Fatalf("dup %s", n)
		}
		seen[n] = struct{}{}
	}
}

func TestSecretGetOmitsData(t *testing.T) {
	sum := summarize("Secret", nil, model.ResourceRef{Kind: "Secret", Name: "s"})
	if m, ok := sum.(map[string]any); ok {
		if _, has := m["data"]; has {
			t.Fatal("data leaked")
		}
	}
}
