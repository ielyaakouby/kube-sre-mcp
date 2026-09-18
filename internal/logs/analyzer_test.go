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

package logs_test

import (
	"strings"
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/logs"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func TestAnalyze(t *testing.T) {
	sigs := logs.Analyze(model.ResourceRef{Kind: "Pod", Name: "p"}, "app", []string{
		"panic: boom",
		"connection refused",
		"no such host",
		"x509: certificate expired",
		"unauthorized token=secret",
	})
	if len(sigs) < 4 {
		t.Fatalf("got %d %#v", len(sigs), sigs)
	}
	fp := logs.Analyze(model.ResourceRef{Kind: "Pod", Name: "p"}, "app", []string{"GET /login HTTP 403"})
	for _, s := range fp {
		if s.Category == "Auth" && strings.Contains(strings.ToLower(s.Message), "registry") {
			t.Fatal("false registry")
		}
		if s.Reason == "auth_failure" && s.Category != "Application" {
			t.Fatalf("want Application auth got %s", s.Category)
		}
	}
}
