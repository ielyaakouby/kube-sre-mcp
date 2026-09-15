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

	"kube-sre-mcp/internal/version"
)

func TestStringAndUserAgent(t *testing.T) {
	got := version.String()
	if got == "" {
		t.Fatal("empty version")
	}
	if strings.HasPrefix(got, "v") {
		t.Fatalf("String() should not keep a leading v: %q", got)
	}
	if strings.HasPrefix(got, "4.") || got == "4.0.0" {
		t.Fatalf("legacy version still present: %q", got)
	}
	ua := version.UserAgent()
	if ua != "kube-sre-mcp/"+got {
		t.Fatalf("UserAgent: got %q want kube-sre-mcp/%s", ua, got)
	}
	if !strings.Contains(ua, "0.1.0-beta.1") && version.Version == "0.1.0-beta.1" {
		t.Fatalf("expected default release in User-Agent: %q", ua)
	}
}
