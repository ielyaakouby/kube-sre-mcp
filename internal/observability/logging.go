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

package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

func NewLogger(level string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv}))
}

func Attrs(requestID, tool, contextName, kind, ns string, dur time.Duration, health, result string) []any {
	return []any{
		"request_id", requestID,
		"tool", tool,
		"cluster_context", contextName,
		"kind", kind,
		"namespace", ns,
		"duration_ms", dur.Milliseconds(),
		"health", health,
		"result", result,
	}
}
