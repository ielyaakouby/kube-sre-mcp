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

package events

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/security"
	"kube-sre-mcp/internal/signal"
)

type Record struct {
	Type           string    `json:"type"`
	Reason         string    `json:"reason"`
	Message        string    `json:"message"`
	Count          int32     `json:"count"`
	FirstTime      time.Time `json:"firstTime"`
	LastTime       time.Time `json:"lastTime"`
	InvolvedObject string    `json:"involvedObject"`
	Namespace      string    `json:"namespace,omitempty"`
	Category       string    `json:"category"`
	Severity       string    `json:"severity"`
	Explanation    string    `json:"explanation,omitempty"`
	Remediation    string    `json:"remediation_hint,omitempty"`
}

type Service struct {
	CS kubernetes.Interface
}

func (s *Service) List(ctx context.Context, namespace, resourceName, resourceKind string, allNamespaces bool, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 30
	}
	fs := fieldSelector(resourceName, resourceKind)
	opts := metav1.ListOptions{FieldSelector: fs}
	var items []corev1.Event
	if allNamespaces || namespace == "" && strings.EqualFold(resourceKind, "Node") {
		list, err := s.CS.CoreV1().Events("").List(ctx, opts)
		if err != nil {
			return nil, err
		}
		items = list.Items
	} else {
		ns := namespace
		if ns == "" {
			ns = "default"
		}
		list, err := s.CS.CoreV1().Events(ns).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		items = list.Items
	}
	out := make([]Record, 0, len(items))
	for i := range items {
		out = append(out, FromK8s(&items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTime.After(out[j].LastTime) })
	if len(out) > limit {
		out = out[:limit]
	}
	return Aggregate(out), nil
}

func fieldSelector(name, kind string) string {
	var parts []string
	if name != "" {
		parts = append(parts, "involvedObject.name="+name)
	}
	if kind != "" {
		parts = append(parts, "involvedObject.kind="+kind)
	}
	return strings.Join(parts, ",")
}

func FromK8s(e *corev1.Event) Record {
	first := timeOr(e.FirstTimestamp.Time, e.EventTime.Time)
	last := timeOr(e.LastTimestamp.Time, e.EventTime.Time)
	if last.IsZero() {
		last = e.CreationTimestamp.Time
	}
	msg := security.Redact(e.Message)
	cat, sev, expl, hint := Classify(e.Reason, msg)
	count := e.Count
	if count == 0 {
		count = 1
	}
	return Record{
		Type:           e.Type,
		Reason:         e.Reason,
		Message:        msg,
		Count:          count,
		FirstTime:      first,
		LastTime:       last,
		InvolvedObject: fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
		Namespace:      e.Namespace,
		Category:       cat,
		Severity:       sev,
		Explanation:    expl,
		Remediation:    hint,
	}
}

func timeOr(a, b time.Time) time.Time {
	if !a.IsZero() {
		return a
	}
	return b
}

func Aggregate(in []Record) []Record {
	type key struct{ obj, reason, msg string }
	seen := map[key]int{}
	var out []Record
	for _, r := range in {
		k := key{r.InvolvedObject, r.Reason, r.Message}
		if i, ok := seen[k]; ok {
			out[i].Count += r.Count
			if r.LastTime.After(out[i].LastTime) {
				out[i].LastTime = r.LastTime
			}
			if !r.FirstTime.IsZero() && (out[i].FirstTime.IsZero() || r.FirstTime.Before(out[i].FirstTime)) {
				out[i].FirstTime = r.FirstTime
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, r)
	}
	return out
}

func ToSignals(ref model.ResourceRef, recs []Record) []signal.DiagnosticSignal {
	var out []signal.DiagnosticSignal
	for i, r := range recs {
		if r.Type != "Warning" && r.Severity == "info" {
			continue
		}
		sev := model.SeverityWarning
		if r.Severity == "critical" {
			sev = model.SeverityCritical
		}
		conf := model.ConfidenceHigh
		id := fmt.Sprintf("E-EVT-%d", i+1)
		ts := r.LastTime
		sig := signal.New(id, signal.SourceEvent, r.Category, r.Reason, sev, conf, ref, fmt.Sprintf("[%s] %s", r.Reason, r.Message))
		sig.Count = int(r.Count)
		sig.Timestamp = &ts
		sig.Metadata = map[string]any{"involvedObject": r.InvolvedObject, "explanation": r.Explanation}
		out = append(out, sig)
	}
	return out
}
