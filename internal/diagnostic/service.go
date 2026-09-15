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
	"strconv"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

	"kube-sre-mcp/internal/graph"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

func DiagnoseService(cluster model.ClusterInfo, svc *corev1.Service, pods []corev1.Pod, slices []discoveryv1.EndpointSlice, endpoints *corev1.Endpoints, podSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal, slicesKnown bool) model.DiagnosticResponse {
	ref := model.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: svc.Namespace, Name: svc.Name, UID: svc.UID}
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-SVC-%d", id), signal.SourceNetwork, "Service", reason, sev, model.ConfidenceHigh, ref, msg))
		id++
	}
	n("service_spec", fmt.Sprintf("type=%s clusterIP=%s ports=%v selector=%v", svc.Spec.Type, svc.Spec.ClusterIP, svc.Spec.Ports, svc.Spec.Selector), model.SeverityInfo)

	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return assemble(cluster, ref, sigs, nil, "full")
	}

	total, ready := graph.EndpointReadyCount(slices)
	if total == 0 && endpoints != nil {
		for _, s := range endpoints.Subsets {
			total += len(s.Addresses) + len(s.NotReadyAddresses)
			ready += len(s.Addresses)
		}
	}

	if len(svc.Spec.Selector) == 0 {
		if svc.Spec.ClusterIP != "None" && svc.Spec.Type != corev1.ServiceTypeExternalName {
			n("SERVICE_NO_SELECTOR", "Service has no selector (ExternalName or manual Endpoints).", model.SeverityWarning)
		}
	} else {
		matching := 0
		readyPods := 0
		for i := range pods {
			matching++
			ok := true
			for _, cs := range pods[i].Status.ContainerStatuses {
				if !cs.Ready {
					ok = false
				}
			}
			if ok && pods[i].Status.Phase == corev1.PodRunning && len(pods[i].Status.ContainerStatuses) > 0 {
				readyPods++
			} else {
				sigs = append(sigs, podSignals[pods[i].Name]...)
			}
			portMismatch(&pods[i], svc, &sigs, &id, ref)
		}
		if matching == 0 {
			n("SERVICE_NO_MATCHING_PODS", "Service selector matches zero pods.", model.SeverityCritical)
		} else if readyPods == 0 {
			n("SERVICE_NO_READY_PODS", fmt.Sprintf("%d pods match selector but none are ready", matching), model.SeverityCritical)
		}
	}
	if slicesKnown && total == 0 && len(svc.Spec.Selector) > 0 {
		n("SERVICE_NO_ENDPOINTS", "Service has no endpoints.", model.SeverityCritical)
	} else if total > 0 && ready == 0 {
		n("SERVICE_NO_READY_ENDPOINTS", "Service has endpoints but none are ready.", model.SeverityCritical)
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	resp.Impact = &model.Impact{ReadyEndpoints: ready, TotalEndpoints: total, Severity: model.ImpactLow}
	if resp.Health == model.HealthCritical {
		resp.Impact.Severity = model.ImpactCritical
	}
	return resp
}

func portMismatch(p *corev1.Pod, svc *corev1.Service, sigs *[]signal.DiagnosticSignal, id *int, ref model.ResourceRef) {
	ports := map[string]int32{}
	nums := map[int32]struct{}{}
	for _, c := range p.Spec.Containers {
		for _, cp := range c.Ports {
			if cp.Name != "" {
				ports[cp.Name] = cp.ContainerPort
			}
			nums[cp.ContainerPort] = struct{}{}
		}
	}
	for _, sp := range svc.Spec.Ports {
		if sp.TargetPort.StrVal != "" {
			if _, ok := ports[sp.TargetPort.StrVal]; !ok {
				*sigs = append(*sigs, signal.New(fmt.Sprintf("E-PORT-%d", *id), signal.SourceNetwork, "Service", "TARGET_PORT_NOT_FOUND", model.SeverityWarning, model.ConfidenceHigh, ref,
					fmt.Sprintf("targetPort name %s not found on pod %s", sp.TargetPort.StrVal, p.Name)))
				*id++
			}
		} else if sp.TargetPort.IntVal != 0 {
			if _, ok := nums[sp.TargetPort.IntVal]; !ok && len(nums) > 0 {
				*sigs = append(*sigs, signal.New(fmt.Sprintf("E-PORT-%d", *id), signal.SourceNetwork, "Service", "SERVICE_PORT_MISMATCH", model.SeverityWarning, model.ConfidenceMedium, ref,
					fmt.Sprintf("targetPort %d not listed on pod %s", sp.TargetPort.IntVal, p.Name)))
				*id++
			}
		}
		_ = strconv.Itoa
	}
}
