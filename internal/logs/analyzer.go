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

package logs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

type pattern struct {
	re       *regexp.Regexp
	category string
	reason   string
	severity model.Severity
}

var patterns = []pattern{
	{regexp.MustCompile(`(?i)\bpanic\b`), "Application", "panic", model.SeverityCritical},
	{regexp.MustCompile(`(?i)\bfatal\b`), "Application", "fatal", model.SeverityCritical},
	{regexp.MustCompile(`(?i)\bexception\b`), "Application", "exception", model.SeverityWarning},
	{regexp.MustCompile(`(?i)\btraceback\b`), "Application", "traceback", model.SeverityWarning},
	{regexp.MustCompile(`(?i)segmentation fault`), "Application", "segfault", model.SeverityCritical},
	{regexp.MustCompile(`(?i)uncaught exception`), "Application", "uncaught_exception", model.SeverityCritical},
	{regexp.MustCompile(`(?i)out of memory|java heap space|allocation failure`), "Memory", "oom", model.SeverityCritical},
	{regexp.MustCompile(`(?i)connection refused`), "Network", "connection_refused", model.SeverityWarning},
	{regexp.MustCompile(`(?i)connection reset`), "Network", "connection_reset", model.SeverityWarning},
	{regexp.MustCompile(`(?i)connection timed out|connection timeout`), "Network", "connection_timeout", model.SeverityWarning},
	{regexp.MustCompile(`(?i)no route to host`), "Network", "no_route", model.SeverityWarning},
	{regexp.MustCompile(`(?i)no such host|nxdomain|temporary failure in name resolution`), "DNS", "dns_failure", model.SeverityWarning},
	{regexp.MustCompile(`(?i)x509|certificate expired|certificate verify failed|tls handshake`), "TLS", "tls_error", model.SeverityWarning},
	{regexp.MustCompile(`(?i)unauthorized|forbidden|access denied|permission denied|\b401\b|\b403\b`), "Application", "auth_failure", model.SeverityWarning},
	{regexp.MustCompile(`(?i)no space left on device|read-only filesystem`), "Filesystem", "disk", model.SeverityCritical},
	{regexp.MustCompile(`(?i)too many connections|database connection refused|database connection timeout`), "Database", "db_connection", model.SeverityWarning},
	{regexp.MustCompile(`(?i)invalid configuration|required environment variable missing|no such file or directory`), "Configuration", "config", model.SeverityWarning},
}

func Analyze(ref model.ResourceRef, container string, lines []string) []signal.DiagnosticSignal {
	if len(lines) == 0 {
		return nil
	}
	seen := map[string]int{}
	samples := map[string]string{}
	for _, line := range lines {
		for _, p := range patterns {
			if p.re.MatchString(line) {
				seen[p.reason]++
				if _, ok := samples[p.reason]; !ok {
					samples[p.reason] = truncate(line, 240)
				}
			}
		}
	}
	var out []signal.DiagnosticSignal
	i := 1
	for _, p := range patterns {
		n := seen[p.reason]
		if n == 0 {
			continue
		}
		id := fmt.Sprintf("E-LOG-%d", i)
		i++
		msg := fmt.Sprintf("container %s: %s matched %d time(s): %s", container, p.reason, n, samples[p.reason])
		sig := signal.New(id, signal.SourceLog, p.category, p.reason, p.severity, model.ConfidenceLow, ref, msg)
		sig.Count = n
		sig.Metadata = map[string]any{"container": container}
		out = append(out, sig)
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
