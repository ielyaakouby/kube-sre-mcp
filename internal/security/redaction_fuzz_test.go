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
	"unicode/utf8"

	"kube-sre-mcp/internal/security"
)

func TestRedactTable(t *testing.T) {
	cases := []struct {
		in   string
		deny string
	}{
		{"Bearer abcdefghijklmnop", "abcdefghijklmnop"},
		{"Authorization: secret-token-value", "secret-token-value"},
		{"password: hunter2", "hunter2"},
		{`{"password":"pw"}`, "pw"},
		{`{"apiKey":"k"}`, `"k"`},
		{"https://user:s3cret@host/x", "s3cret"},
		{"-----BEGIN RSA PRIVATE KEY-----\nABC\n-----END RSA PRIVATE KEY-----", "BEGIN RSA"},
		{`{"auths":{"h":{"auth":"x"}},".dockerconfigjson":"abc"}`, ""},
		{"AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE"},
		{"line1\npassword=abc\nline3", "abc"},
	}
	for _, c := range cases {
		out := security.Redact(c.in)
		if c.deny != "" && strings.Contains(out, c.deny) && !strings.Contains(out, "[REDACTED") {
			t.Fatalf("in %q out %q still contains %q", c.in, out, c.deny)
		}
	}
	ann := security.RedactMap(map[string]string{"kubectl.kubernetes.io/token": "abcd", "note": "ok"})
	if ann["kubectl.kubernetes.io/token"] != "[REDACTED]" {
		t.Fatalf("%#v", ann)
	}
}

func TestRedactNoPanicUTF8(t *testing.T) {
	inputs := []string{"", "\x80\x81", string([]byte{0xff, 0xfe, 0x00}), "🙂 password=x", strings.Repeat("あ", 1000)}
	for _, in := range inputs {
		out := security.Redact(in)
		if out != "" && !utf8.ValidString(out) && utf8.ValidString(in) {
			t.Fatalf("invalid utf8 out")
		}
	}
}

func FuzzRedact(f *testing.F) {
	f.Add("Bearer abc")
	f.Add("password=x")
	f.Add("\x00\x01")
	f.Fuzz(func(t *testing.T, s string) {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("panic %v on %q", rec, s)
			}
		}()
		_ = security.Redact(s)
		_ = security.RedactMap(map[string]string{"k": s, "password": s})
	})
}
