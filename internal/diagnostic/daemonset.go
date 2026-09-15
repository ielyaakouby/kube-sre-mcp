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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"kube-sre-mcp/internal/graph"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

func DiagnoseDaemonSet(cluster model.ClusterInfo, d *appsv1.DaemonSet, pods []corev1.Pod, podSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := graph.DSRef(d)
	desired := d.Status.DesiredNumberScheduled
	ready := d.Status.NumberReady
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-DS-%d", id), signal.SourceStatus, "Workload", reason, sev, model.ConfidenceConfirmed, ref, msg))
		id++
	}
	n("daemonset_status", fmt.Sprintf("desired=%d current=%d ready=%d available=%d unavailable=%d misscheduled=%d",
		desired, d.Status.CurrentNumberScheduled, ready, d.Status.NumberAvailable, d.Status.NumberUnavailable, d.Status.NumberMisscheduled), model.SeverityInfo)
	if ready < desired {
		sev := model.SeverityWarning
		if ready == 0 {
			sev = model.SeverityCritical
		}
		n("InsufficientNodes", fmt.Sprintf("Only %d/%d nodes have ready pods", ready, desired), sev)
	}
	if d.Status.NumberMisscheduled > 0 {
		n("Misscheduled", fmt.Sprintf("%d pods misscheduled", d.Status.NumberMisscheduled), model.SeverityWarning)
	}
	nodes := map[string]string{}
	for _, p := range pods {
		if UnhealthyPod(&p) {
			sigs = append(sigs, podSignals[p.Name]...)
			nodes[p.Spec.NodeName] = DominantWaiting(&p)
		}
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	resp.Impact = &model.Impact{Severity: impactSev(desired, ready), AffectedNodes: len(nodes), DesiredReplicas: int(desired)}
	return resp
}
