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

package security

import (
	"regexp"
	"strings"
)

var (
	reBearer   = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9\-\._~\+\/]+=*`)
	reJWT      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	reAuth     = regexp.MustCompile(`(?i)(Authorization:\s*)\S+`)
	rePass     = regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`)
	reJSONPass = regexp.MustCompile(`(?i)("(?:password|passwd|pwd)"\s*:\s*")[^"]*(")`)
	reJSONTok  = regexp.MustCompile(`(?i)("(?:api[_-]?key|token|access_token|secret)"\s*:\s*")[^"]*(")`)
	reAPIKey   = regexp.MustCompile(`(?i)(api[_-]?key|token|secret)\s*[:=]\s*\S+`)
	reDocker   = regexp.MustCompile(`(?i)(\.dockerconfigjson|dockercfg)\s*[:=]?\s*\S+`)
	rePEM      = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)
	reBasicURL = regexp.MustCompile(`(?i)(https?://)([^:@\s/]+):([^@\s/]+)@`)
	reAWSKey   = regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`)
)

func Redact(s string) string {
	if s == "" {
		return s
	}
	out := rePEM.ReplaceAllString(s, "[REDACTED_PRIVATE_KEY]")
	out = reJWT.ReplaceAllString(out, "[REDACTED_JWT]")
	out = reBearer.ReplaceAllString(out, "${1}[REDACTED]")
	out = reAuth.ReplaceAllString(out, "${1}[REDACTED]")
	out = reJSONPass.ReplaceAllString(out, `${1}[REDACTED]$2`)
	out = reJSONTok.ReplaceAllString(out, `${1}[REDACTED]$2`)
	out = rePass.ReplaceAllString(out, "${1}=[REDACTED]")
	out = reAPIKey.ReplaceAllString(out, "${1}=[REDACTED]")
	out = reDocker.ReplaceAllString(out, "${1}=[REDACTED]")
	out = reBasicURL.ReplaceAllString(out, "${1}${2}:[REDACTED]@")
	out = reAWSKey.ReplaceAllString(out, "[REDACTED_AWS_KEY]")
	if strings.Contains(strings.ToLower(out), `"data"`) && strings.Contains(strings.ToLower(out), "secret") {
		out = "[REDACTED_SECRET_OBJECT]"
	}
	return out
}

func RedactMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") || strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.Contains(lk, "key") {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = Redact(v)
	}
	return out
}

func SecretSummary(name, namespace, typ string, keys []string, created string) map[string]any {
	return map[string]any{
		"kind":              "Secret",
		"name":              name,
		"namespace":         namespace,
		"type":              typ,
		"keys":              keys,
		"creationTimestamp": created,
		"note":              "Secret values are not returned for security reasons.",
	}
}
