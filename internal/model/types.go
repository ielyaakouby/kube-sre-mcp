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

package model

import (
	"time"

	"k8s.io/apimachinery/pkg/types"
)

type Health string

const (
	HealthHealthy  Health = "healthy"
	HealthDegraded Health = "degraded"
	HealthCritical Health = "critical"
	HealthUnknown  Health = "unknown"
)

type Confidence string

const (
	ConfidenceLow       Confidence = "low"
	ConfidenceMedium    Confidence = "medium"
	ConfidenceHigh      Confidence = "high"
	ConfidenceConfirmed Confidence = "confirmed"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type ImpactSeverity string

const (
	ImpactLow      ImpactSeverity = "low"
	ImpactMedium   ImpactSeverity = "medium"
	ImpactHigh     ImpactSeverity = "high"
	ImpactCritical ImpactSeverity = "critical"
)

type ResourceRef struct {
	APIVersion string    `json:"apiVersion,omitempty"`
	Kind       string    `json:"kind"`
	Namespace  string    `json:"namespace,omitempty"`
	Name       string    `json:"name"`
	UID        types.UID `json:"uid,omitempty"`
}

func (r ResourceRef) String() string {
	if r.Namespace == "" {
		return r.Kind + "/" + r.Name
	}
	return r.Kind + "/" + r.Namespace + "/" + r.Name
}

type ClusterInfo struct {
	Context   string `json:"context"`
	Server    string `json:"server,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
}

type Finding struct {
	Severity  Severity `json:"severity"`
	Reason    string   `json:"reason"`
	Container string   `json:"container,omitempty"`
	Message   string   `json:"message"`
}

type Evidence struct {
	ID        string      `json:"id,omitempty"`
	Source    string      `json:"source"`
	Message   string      `json:"message"`
	Resource  ResourceRef `json:"resource,omitempty"`
	Timestamp *time.Time  `json:"timestamp,omitempty"`
}

type RecommendedAction struct {
	Priority   int    `json:"priority"`
	Action     string `json:"action"`
	Tool       string `json:"tool,omitempty"`
	Command    string `json:"command,omitempty"`
	RestartsOK bool   `json:"restart_suggested,omitempty"`
}

type RootCauseHypothesis struct {
	Category                 string              `json:"category"`
	Title                    string              `json:"title,omitempty"`
	Explanation              string              `json:"explanation,omitempty"`
	Message                  string              `json:"message,omitempty"`
	Confidence               Confidence          `json:"confidence"`
	AffectedResources        []ResourceRef       `json:"affected_resources,omitempty"`
	SupportingEvidenceIDs    []string            `json:"supporting_evidence,omitempty"`
	ContradictingEvidenceIDs []string            `json:"contradicting_evidence,omitempty"`
	Impact                   *Impact             `json:"impact,omitempty"`
	Recommendations          []RecommendedAction `json:"recommendations,omitempty"`
	AffectedReplicas         int                 `json:"affected_replicas,omitempty"`
}

type Impact struct {
	Severity            ImpactSeverity `json:"severity"`
	UnavailableReplicas int            `json:"unavailable_replicas,omitempty"`
	DesiredReplicas     int            `json:"desired_replicas,omitempty"`
	PercentImpacted     float64        `json:"percent_impacted,omitempty"`
	AffectedNodes       int            `json:"affected_nodes,omitempty"`
	ReadyEndpoints      int            `json:"ready_endpoints,omitempty"`
	TotalEndpoints      int            `json:"total_endpoints,omitempty"`
	WorkloadsOnNode     int            `json:"workloads_on_node,omitempty"`
	Summary             string         `json:"summary,omitempty"`
}

type FailedCheck struct {
	Operation string `json:"operation"`
	Resource  string `json:"resource"`
	ErrorType string `json:"error_type"`
	Message   string `json:"message,omitempty"`
}

type Visibility struct {
	Complete           bool          `json:"complete"`
	Limited            bool          `json:"limited"`
	MissingPermissions []Permission  `json:"missing_permissions,omitempty"`
	FailedChecks       []FailedCheck `json:"failed_checks,omitempty"`
}

func NewVisibility() *Visibility {
	return &Visibility{Complete: true}
}

type Permission struct {
	Verb      string `json:"verb"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace,omitempty"`
	Message   string `json:"message,omitempty"`
}

type Dependency struct {
	Type     string      `json:"type"`
	Resource ResourceRef `json:"resource"`
	Status   string      `json:"status,omitempty"`
	Message  string      `json:"message,omitempty"`
}

type ActionPlan struct {
	ID                   string      `json:"id,omitempty"`
	Action               string      `json:"action"`
	Target               ResourceRef `json:"target"`
	Reason               string      `json:"reason,omitempty"`
	Risk                 string      `json:"risk"`
	BlastRadius          any         `json:"blast_radius,omitempty"`
	Preconditions        []string    `json:"preconditions,omitempty"`
	RequiresConfirmation bool        `json:"requires_confirmation"`
	Authorized           bool        `json:"authorized"`
}

type SchedulerAnalysis struct {
	Completeness string   `json:"completeness"`
	Mode         string   `json:"mode,omitempty"`
	Unsupported  []string `json:"unsupported,omitempty"`
}

type DiagnosticResponse struct {
	Status                 string                `json:"status"`
	Cluster                ClusterInfo           `json:"cluster"`
	Target                 ResourceRef           `json:"target"`
	Health                 Health                `json:"health"`
	Summary                string                `json:"summary"`
	Impact                 *Impact               `json:"impact,omitempty"`
	RootCause              *RootCauseHypothesis  `json:"root_cause,omitempty"`
	RootCauses             []RootCauseHypothesis `json:"root_causes,omitempty"`
	PossibleCauses         []string              `json:"possible_causes,omitempty"`
	Findings               []Finding             `json:"findings"`
	Evidence               []Evidence            `json:"evidence"`
	Dependencies           []Dependency          `json:"dependencies,omitempty"`
	Recommendations        []RecommendedAction   `json:"recommended_actions"`
	AvailableActions       []ActionPlan          `json:"available_actions,omitempty"`
	Visibility             *Visibility           `json:"visibility,omitempty"`
	DiagnosticDepth        string                `json:"diagnostic_depth,omitempty"`
	Truncated              bool                  `json:"truncated,omitempty"`
	TruncationReason       string                `json:"truncation_reason,omitempty"`
	MetricsAvailable       *bool                 `json:"metrics_available,omitempty"`
	ScheduleMode           string                `json:"schedule_mode,omitempty"`
	SchedulerAnalysis      *SchedulerAnalysis    `json:"scheduler_analysis,omitempty"`
	Resolution             string                `json:"resolution,omitempty"`
	DeepDiagnosisTruncated bool                  `json:"deep_diagnosis_truncated,omitempty"`
	DeepDiagnosisSkipped   int                   `json:"deep_diagnosis_skipped,omitempty"`
	Commands               []string              `json:"commands,omitempty"`
	Raw                    any                   `json:"raw,omitempty"`
	Message                string                `json:"message,omitempty"`
}
