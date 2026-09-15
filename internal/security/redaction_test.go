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

package security_test

import (
	"strings"
	"testing"

	"kube-sre-mcp/internal/security"
)

func TestRedact(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	out := security.Redact("Authorization: Bearer abcdef.ghij.klmn password=supersecret api_key=abcd " + jwt)
	if strings.Contains(out, "supersecret") || strings.Contains(out, "abcdef.ghij") || strings.Contains(out, jwt) {
		t.Fatalf("not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED") {
		t.Fatalf("expected redaction markers: %s", out)
	}
	out2 := security.Redact(`{"password":"hunter2"} https://user:s3cret@host AKIAIOSFODNN7EXAMPLE -----BEGIN RSA PRIVATE KEY-----abc-----END RSA PRIVATE KEY-----`)
	if strings.Contains(out2, "hunter2") || strings.Contains(out2, "s3cret") || strings.Contains(out2, "AKIAIOSFODNN7EXAMPLE") || strings.Contains(out2, "BEGIN RSA") {
		t.Fatalf("json/url/key: %s", out2)
	}
}
