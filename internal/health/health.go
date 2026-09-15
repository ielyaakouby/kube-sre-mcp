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

package health

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

// Evaluate is the single health contract used by resource diagnosis and cluster health.
func Evaluate(signals []signal.DiagnosticSignal, visibility *model.Visibility) model.Health {
	if visibility != nil && visibility.Limited && requiredMissing(visibility) {
		hasCritical := false
		for _, s := range signals {
			if s.Severity == model.SeverityCritical {
				hasCritical = true
				break
			}
		}
		if !hasCritical && len(signals) == 0 {
			return model.HealthUnknown
		}
	}
	worst := model.SeverityInfo
	hasCrit := false
	hasWarn := false
	for _, s := range signals {
		if s.Severity == model.SeverityCritical {
			hasCrit = true
		}
		if s.Severity == model.SeverityWarning {
			hasWarn = true
		}
		worst = signal.MaxSeverity(worst, s.Severity)
	}
	if hasCrit {
		return model.HealthCritical
	}
	if hasWarn {
		return model.HealthDegraded
	}
	if visibility != nil && visibility.Limited && len(signals) == 0 {
		return model.HealthUnknown
	}
	return model.HealthHealthy
}

func requiredMissing(v *model.Visibility) bool {
	return len(v.MissingPermissions) > 0 || len(v.FailedChecks) > 0
}

func Combine(states ...model.Health) model.Health {
	hasUnknown := false
	hasCrit := false
	hasDeg := false
	for _, h := range states {
		switch h {
		case model.HealthCritical:
			hasCrit = true
		case model.HealthDegraded:
			hasDeg = true
		case model.HealthUnknown:
			hasUnknown = true
		}
	}
	if hasCrit {
		return model.HealthCritical
	}
	if hasDeg {
		return model.HealthDegraded
	}
	if hasUnknown {
		return model.HealthUnknown
	}
	return model.HealthHealthy
}

func FromCounts(desired, ready int, failed bool) model.Health {
	if failed || (desired > 0 && ready == 0) {
		return model.HealthCritical
	}
	if ready < desired {
		return model.HealthDegraded
	}
	return model.HealthHealthy
}

func FromPod(p *corev1.Pod) model.Health {
	return Evaluate(PodStatusSignals(p), nil)
}

func FromNode(n *corev1.Node) model.Health {
	return Evaluate(NodeStatusSignals(n), nil)
}

func NodeStatusSignals(n *corev1.Node) []signal.DiagnosticSignal {
	if n == nil {
		return nil
	}
	ref := model.ResourceRef{Kind: "Node", Name: n.Name, UID: n.UID}
	var sigs []signal.DiagnosticSignal
	for i, c := range n.Status.Conditions {
		healthy := (c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue) ||
			(c.Type != corev1.NodeReady && c.Status == corev1.ConditionFalse)
		if !healthy {
			sigs = append(sigs, signal.New(fmt.Sprintf("E-NODE-%d", i), signal.SourceCondition, "Node", string(c.Type), model.SeverityCritical, model.ConfidenceConfirmed, ref, c.Message))
		}
	}
	if n.Spec.Unschedulable {
		sigs = append(sigs, signal.New("E-NODE-CORDON", signal.SourceStatus, "Node", "Cordoned", model.SeverityWarning, model.ConfidenceConfirmed, ref, "cordoned"))
	}
	return sigs
}

func PodStatusSignals(p *corev1.Pod) []signal.DiagnosticSignal {
	if p == nil {
		return nil
	}
	ref := model.ResourceRef{Kind: "Pod", Name: p.Name, Namespace: p.Namespace, UID: p.UID}
	var sigs []signal.DiagnosticSignal
	id := 1
	next := func() string {
		s := fmt.Sprintf("E-INV-%d", id)
		id++
		return s
	}
	if p.Status.Phase == corev1.PodSucceeded {
		return nil
	}
	if p.Status.Phase == corev1.PodFailed {
		sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Pod", string(p.Status.Phase), model.SeverityCritical, model.ConfidenceConfirmed, ref, "pod failed"))
	}
	if p.Status.Phase == corev1.PodPending {
		sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Pod", "Pending", model.SeverityCritical, model.ConfidenceHigh, ref, "pod pending"))
	}
	all := append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...)
	all = append(all, p.Status.ContainerStatuses...)
	for _, cs := range all {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", w.Reason, model.SeverityCritical, model.ConfidenceConfirmed, ref, w.Reason))
		}
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			reason := t.Reason
			if reason == "" {
				reason = "Error"
			}
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", reason, model.SeverityCritical, model.ConfidenceConfirmed, ref, reason))
		}
		if ls := cs.LastTerminationState.Terminated; ls != nil && ls.Reason == "OOMKilled" {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Memory", "OOMKilled", model.SeverityCritical, model.ConfidenceConfirmed, ref, "OOMKilled"))
		}
		if cs.RestartCount > 5 {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", "Restarts", model.SeverityCritical, model.ConfidenceHigh, ref, "high restarts"))
		} else if cs.RestartCount > 0 {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", "Restarts", model.SeverityWarning, model.ConfidenceHigh, ref, "restarts"))
		}
		if p.Status.Phase == corev1.PodRunning && !cs.Ready {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Probe", "READINESS_PROBE_FAILED", model.SeverityWarning, model.ConfidenceHigh, ref, "container not ready"))
		}
	}
	return sigs
}
