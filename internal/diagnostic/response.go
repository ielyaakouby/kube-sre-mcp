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
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func okResp(cluster model.ClusterInfo, target model.ResourceRef, health model.Health, summary string) model.DiagnosticResponse {
	return model.DiagnosticResponse{
		Status:          "success",
		Cluster:         cluster,
		Target:          target,
		Health:          health,
		Summary:         summary,
		Findings:        []model.Finding{},
		Evidence:        []model.Evidence{},
		Recommendations: []model.RecommendedAction{},
		DiagnosticDepth: "full",
	}
}

func errorResp(cluster model.ClusterInfo, target model.ResourceRef, msg string) model.DiagnosticResponse {
	return model.DiagnosticResponse{
		Status: "error", Cluster: cluster, Target: target,
		Health: model.HealthUnknown, Summary: msg,
		Findings: []model.Finding{}, Evidence: []model.Evidence{},
		Recommendations: []model.RecommendedAction{},
	}
}

func permissionResp(cluster model.ClusterInfo, target model.ResourceRef, vis *model.Visibility, msg string) model.DiagnosticResponse {
	return model.DiagnosticResponse{
		Status: "error", Cluster: cluster, Target: target,
		Health: model.HealthUnknown, Summary: msg,
		RootCause: &model.RootCauseHypothesis{
			Category: "permission_denied", Message: msg, Confidence: model.ConfidenceConfirmed,
		},
		Findings:        []model.Finding{{Severity: model.SeverityCritical, Reason: "RBAC", Message: msg}},
		Evidence:        []model.Evidence{{Source: "rbac", Message: msg}},
		Recommendations: []model.RecommendedAction{{Priority: 1, Action: "Check RBAC permissions for the current Kubernetes identity."}},
		Visibility:      vis,
	}
}

func notFoundResp(cluster model.ClusterInfo, target model.ResourceRef, msg string) model.DiagnosticResponse {
	return model.DiagnosticResponse{
		Status: "error", Cluster: cluster, Target: target,
		Health: model.HealthUnknown, Summary: msg,
		Findings: []model.Finding{{Severity: model.SeverityCritical, Reason: "NotFound", Message: msg}},
		Evidence: []model.Evidence{}, Recommendations: []model.RecommendedAction{},
	}
}

func boolPtr(v bool) *bool { return &v }
