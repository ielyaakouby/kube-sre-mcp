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

	corev1 "k8s.io/api/core/v1"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func DiagnoseNode(cluster model.ClusterInfo, n *corev1.Node, hosted int, ev []signal.DiagnosticSignal, metricsAvail bool) model.DiagnosticResponse {
	ref := model.ResourceRef{APIVersion: "v1", Kind: "Node", Name: n.Name, UID: n.UID}
	var sigs []signal.DiagnosticSignal
	id := 1
	add := func(src signal.Source, reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-NODE-%d", id), src, "Node", reason, sev, model.ConfidenceConfirmed, ref, msg))
		id++
	}
	for _, c := range n.Status.Conditions {
		healthy := (c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue) ||
			(c.Type != corev1.NodeReady && c.Status == corev1.ConditionFalse)
		if !healthy {
			add(signal.SourceCondition, string(c.Type), fmt.Sprintf("%s=%s: %s", c.Type, c.Status, c.Message), model.SeverityCritical)
		} else {
			add(signal.SourceCondition, string(c.Type), fmt.Sprintf("%s=%s", c.Type, c.Status), model.SeverityInfo)
		}
	}
	if n.Spec.Unschedulable {
		add(signal.SourceStatus, "Cordoned", "Node is cordoned (unschedulable).", model.SeverityWarning)
	}
	cap := n.Status.Capacity
	alloc := n.Status.Allocatable
	add(signal.SourceStatus, "resources", fmt.Sprintf("capacity cpu=%s mem=%s | allocatable cpu=%s mem=%s",
		cap.Cpu(), cap.Memory(), alloc.Cpu(), alloc.Memory()), model.SeverityInfo)
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	resp.MetricsAvailable = boolPtr(metricsAvail)
	resp.Impact = &model.Impact{WorkloadsOnNode: hosted, Severity: model.ImpactLow}
	if resp.Health == model.HealthCritical {
		resp.Impact.Severity = model.ImpactCritical
	}
	return resp
}
