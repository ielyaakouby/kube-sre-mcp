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

package version_test

import (
	"strings"
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/version"
)

func TestStringAndUserAgent(t *testing.T) {
	got := version.String()
	if got == "" {
		t.Fatal("empty version")
	}
	if !strings.HasPrefix(got, "v") {
		t.Fatalf("String() should report a leading v: %q", got)
	}
	if strings.Contains(got, "beta") || got == "v1.0.0" || got == "1.0.0" {
		t.Fatalf("stale version still present: %q", got)
	}
	if got != "v0.1.0" && version.Version == "0.1.0" {
		t.Fatalf("expected default release v0.1.0, got %q", got)
	}
	ua := version.UserAgent()
	wantUA := "kube-sre-mcp/" + strings.TrimPrefix(version.Version, "v")
	if ua != wantUA {
		t.Fatalf("UserAgent: got %q want %q", ua, wantUA)
	}
	if !strings.Contains(ua, "0.1.0") {
		t.Fatalf("expected 0.1.0 in User-Agent: %q", ua)
	}
	if strings.Contains(ua, "v0.1.0") {
		t.Fatalf("User-Agent should not double the v prefix: %q", ua)
	}
}
