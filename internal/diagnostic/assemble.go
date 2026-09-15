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

package diagnostic

import (
	"fmt"
	"sort"
	"strings"

	"kube-sre-mcp/internal/correlation"
	"kube-sre-mcp/internal/health"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/recommendation"
	"kube-sre-mcp/internal/signal"
)

func assemble(cluster model.ClusterInfo, target model.ResourceRef, sigs []signal.DiagnosticSignal, vis *model.Visibility, depth string) model.DiagnosticResponse {
	h := health.Evaluate(sigs, vis)
	impact := impactFrom(target, sigs, h)
	hyps := correlation.Correlate(sigs, impact)
	scopeIDs(target, sigs, hyps)
	var findings []model.Finding
	var evidence []model.Evidence
	for _, s := range sigs {
		if s.Severity != model.SeverityInfo {
			findings = append(findings, model.Finding{Severity: s.Severity, Reason: s.Reason, Message: s.Message})
		}
		evidence = append(evidence, s.ToEvidence())
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Reason != findings[j].Reason {
			return findings[i].Reason < findings[j].Reason
		}
		return findings[i].Message < findings[j].Message
	})
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].ID != evidence[j].ID {
			return evidence[i].ID < evidence[j].ID
		}
		return evidence[i].Message < evidence[j].Message
	})
	resp := model.DiagnosticResponse{
		Status:          "success",
		Cluster:         cluster,
		Target:          target,
		Health:          h,
		Summary:         summaryFrom(target, h, hyps),
		Impact:          impact,
		RootCauses:      hyps,
		Findings:        findings,
		Evidence:        evidence,
		Recommendations: recsFrom(hyps),
		Visibility:      vis,
		DiagnosticDepth: depth,
	}
	if vis != nil && vis.Limited {
		resp.Status = "partial"
	}
	if len(hyps) > 0 {
		rc := hyps[0]
		resp.RootCause = &rc
	}
	if h == model.HealthHealthy {
		resp.RootCause = nil
	}
	return resp
}

func summaryFrom(t model.ResourceRef, h model.Health, hyps []model.RootCauseHypothesis) string {
	if h == model.HealthHealthy {
		return fmt.Sprintf("%s %s appears healthy.", t.Kind, t.Name)
	}
	if len(hyps) > 0 {
		return fmt.Sprintf("%s %s is %s: %s", t.Kind, t.Name, h, hyps[0].Category)
	}
	return fmt.Sprintf("%s %s is %s.", t.Kind, t.Name, h)
}

func recsFrom(hyps []model.RootCauseHypothesis) []model.RecommendedAction {
	if len(hyps) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []model.RecommendedAction
	for _, h := range hyps {
		for _, r := range h.Recommendations {
			if _, ok := seen[r.Action]; ok {
				continue
			}
			seen[r.Action] = struct{}{}
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return recommendation.For("")
	}
	return out
}

func impactFrom(t model.ResourceRef, sigs []signal.DiagnosticSignal, h model.Health) *model.Impact {
	sev := model.ImpactLow
	switch h {
	case model.HealthCritical:
		sev = model.ImpactCritical
	case model.HealthDegraded:
		sev = model.ImpactHigh
	}
	return &model.Impact{Severity: sev, Summary: strings.ToLower(string(h)) + " " + t.Kind}
}

func scopeIDs(target model.ResourceRef, sigs []signal.DiagnosticSignal, hyps []model.RootCauseHypothesis) {
	prefix := target.String() + ":"
	mapID := func(id string) string {
		if id == "" || strings.Contains(id, ":") {
			return id
		}
		return prefix + id
	}
	for i := range sigs {
		sigs[i].ID = mapID(sigs[i].ID)
	}
	for i := range hyps {
		for j, id := range hyps[i].SupportingEvidenceIDs {
			hyps[i].SupportingEvidenceIDs[j] = mapID(id)
		}
		for j, id := range hyps[i].ContradictingEvidenceIDs {
			hyps[i].ContradictingEvidenceIDs[j] = mapID(id)
		}
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
