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

func DiagnoseStatefulSet(cluster model.ClusterInfo, s *appsv1.StatefulSet, pods []corev1.Pod, pvcs []corev1.PersistentVolumeClaim, podSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := graph.STSRef(s)
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	ready := s.Status.ReadyReplicas
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-STS-%d", id), signal.SourceStatus, "Workload", reason, sev, model.ConfidenceConfirmed, ref, msg))
		id++
	}
	n("replica_status", fmt.Sprintf("desired=%d ready=%d current=%d updated=%d currentRevision=%s updateRevision=%s",
		desired, ready, s.Status.CurrentReplicas, s.Status.UpdatedReplicas, s.Status.CurrentRevision, s.Status.UpdateRevision), model.SeverityInfo)
	if ready < desired {
		sev := model.SeverityWarning
		if ready == 0 {
			sev = model.SeverityCritical
		}
		n("InsufficientReplicas", fmt.Sprintf("Only %d/%d replicas ready", ready, desired), sev)
	}
	if s.Status.CurrentRevision != "" && s.Status.UpdateRevision != "" && s.Status.CurrentRevision != s.Status.UpdateRevision && ready < desired {
		n("revision_mismatch", "currentRevision differs from updateRevision while replicas are not ready (rollout stuck)", model.SeverityWarning)
	}
	for i := range pvcs {
		if pvcs[i].Status.Phase != corev1.ClaimBound {
			n("PVC_PENDING", fmt.Sprintf("PVC %s phase=%s", pvcs[i].Name, pvcs[i].Status.Phase), model.SeverityCritical)
		}
	}
	for _, p := range pods {
		if UnhealthyPod(&p) {
			sigs = append(sigs, podSignals[p.Name]...)
		}
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	resp.Impact = &model.Impact{Severity: impactSev(desired, ready), UnavailableReplicas: int(desired - ready), DesiredReplicas: int(desired)}
	return resp
}
