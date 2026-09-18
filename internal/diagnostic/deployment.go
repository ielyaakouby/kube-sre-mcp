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

	"github.com/ielyaakouby/kube-sre-mcp/internal/graph"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func DiagnoseDeployment(cluster model.ClusterInfo, d *appsv1.Deployment, pods []corev1.Pod, podSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := graph.DeployRef(d)
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	avail := d.Status.AvailableReplicas
	unavail := d.Status.UnavailableReplicas
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) signal.DiagnosticSignal {
		s := fmt.Sprintf("E-DEP-%d", id)
		id++
		return signal.New(s, signal.SourceStatus, "Workload", reason, sev, model.ConfidenceConfirmed, ref, msg)
	}
	sigs = append(sigs, n("replica_status", fmt.Sprintf("desired=%d ready=%d available=%d unavailable=%d updated=%d", desired, ready, avail, unavail, d.Status.UpdatedReplicas), model.SeverityInfo))
	if ready < desired {
		sev := model.SeverityWarning
		if ready == 0 {
			sev = model.SeverityCritical
		}
		sigs = append(sigs, n("InsufficientReplicas", fmt.Sprintf("Only %d/%d replicas are ready", ready, desired), sev))
	}
	for _, c := range d.Status.Conditions {
		if c.Status == corev1.ConditionFalse {
			sigs = append(sigs, n(c.Reason, fmt.Sprintf("%s=False: %s", c.Type, c.Message), model.SeverityCritical))
		}
		if c.Reason == "ProgressDeadlineExceeded" {
			sigs = append(sigs, n("ProgressDeadlineExceeded", c.Message, model.SeverityCritical))
		}
	}
	counts := map[string]int{}
	for _, p := range pods {
		if !UnhealthyPod(&p) {
			continue
		}
		ss := podSignals[p.Name]
		sigs = append(sigs, ss...)
		if len(ss) == 0 {
			counts[DominantWaiting(&p)]++
		} else {
			counts[ss[0].Reason]++
		}
	}
	var majority string
	max := 0
	for k, v := range counts {
		if v > max {
			max = v
			majority = k
		}
	}
	if majority != "" && max > 0 {
		cat := majority
		msg := waitingMessageFor(pods, majority)
		sigs = append(sigs, signal.New(fmt.Sprintf("E-AGG-%d", id), signal.SourceStatus, "Workload", cat, model.SeverityCritical, model.ConfidenceHigh, ref,
			fmt.Sprintf("%d replica(s) share reason %s; %s", max, cat, msg)))
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	resp.Impact = &model.Impact{
		Severity:            impactSev(desired, ready),
		UnavailableReplicas: int(unavail),
		DesiredReplicas:     int(desired),
		PercentImpacted:     pct(int(desired-ready), int(desired)),
		Summary:             fmt.Sprintf("%d/%d replicas ready", ready, desired),
	}
	if resp.RootCause != nil {
		resp.RootCause.AffectedReplicas = max
		resp.RootCause.Impact = resp.Impact
	}
	if resp.Health == model.HealthHealthy {
		resp.Summary = fmt.Sprintf("Deployment %s is healthy with %d/%d replicas ready.", d.Name, ready, desired)
	}
	return resp
}

func waitingMessageFor(pods []corev1.Pod, reason string) string {
	for i := range pods {
		if DominantWaiting(&pods[i]) == reason {
			return waitingMessage(&pods[i])
		}
	}
	return ""
}

func impactSev(desired, ready int32) model.ImpactSeverity {
	if desired > 0 && ready == 0 {
		return model.ImpactCritical
	}
	if ready < desired {
		return model.ImpactHigh
	}
	return model.ImpactLow
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) * 100 / float64(d)
}
