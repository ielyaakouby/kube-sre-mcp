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

package signal

import (
	"time"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

type Source string

const (
	SourceStatus     Source = "STATUS"
	SourceCondition  Source = "CONDITION"
	SourceEvent      Source = "EVENT"
	SourceLog        Source = "LOG"
	SourceMetric     Source = "METRIC"
	SourceDependency Source = "DEPENDENCY"
	SourceScheduler  Source = "SCHEDULER"
	SourceStorage    Source = "STORAGE"
	SourceNetwork    Source = "NETWORK"
	SourceSecurity   Source = "SECURITY"
)

type DiagnosticSignal struct {
	ID         string            `json:"id"`
	Source     Source            `json:"source"`
	Category   string            `json:"category"`
	Reason     string            `json:"reason"`
	Severity   model.Severity    `json:"severity"`
	Confidence model.Confidence  `json:"confidence"`
	Resource   model.ResourceRef `json:"resource"`
	Message    string            `json:"message"`
	Timestamp  *time.Time        `json:"timestamp,omitempty"`
	Count      int               `json:"count,omitempty"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
}

func (s DiagnosticSignal) ToEvidence() model.Evidence {
	return model.Evidence{
		ID:        s.ID,
		Source:    string(s.Source),
		Message:   s.Message,
		Resource:  s.Resource,
		Timestamp: s.Timestamp,
	}
}

func New(id string, src Source, category, reason string, sev model.Severity, conf model.Confidence, ref model.ResourceRef, msg string) DiagnosticSignal {
	return DiagnosticSignal{
		ID: id, Source: src, Category: category, Reason: reason,
		Severity: sev, Confidence: conf, Resource: ref, Message: msg, Count: 1,
	}
}
