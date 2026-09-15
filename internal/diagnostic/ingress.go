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
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"kube-sre-mcp/internal/graph"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

func DiagnoseIngress(cluster model.ClusterInfo, ing *networkingv1.Ingress, services map[string]*corev1.Service, slices map[string][]discoveryv1.EndpointSlice, tlsSecrets map[string]bool, classExists *bool, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := model.ResourceRef{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Namespace: ing.Namespace, Name: ing.Name, UID: ing.UID}
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-ING-%d", id), signal.SourceNetwork, "Ingress", reason, sev, model.ConfidenceHigh, ref, msg))
		id++
	}
	cls := ""
	if ing.Spec.IngressClassName != nil {
		cls = *ing.Spec.IngressClassName
	}
	n("ingress_spec", fmt.Sprintf("ingressClassName=%s rules=%d tls=%d", cls, len(ing.Spec.Rules), len(ing.Spec.TLS)), model.SeverityInfo)
	if cls != "" && classExists != nil && !*classExists {
		n("INGRESS_CLASS_MISSING", fmt.Sprintf("IngressClass %s not found", cls), model.SeverityWarning)
	}

	checkBackend := func(svc *networkingv1.IngressServiceBackend) {
		if svc == nil {
			return
		}
		obj, ok := services[svc.Name]
		if !ok {
			n("INGRESS_BACKEND_MISSING", fmt.Sprintf("Backend Service %s not found", svc.Name), model.SeverityCritical)
			return
		}
		if svc.Port.Number != 0 {
			found := false
			for _, p := range obj.Spec.Ports {
				if p.Port == svc.Port.Number {
					found = true
				}
			}
			if !found {
				n("INGRESS_BACKEND_PORT_INVALID", fmt.Sprintf("Service %s has no port %d", svc.Name, svc.Port.Number), model.SeverityCritical)
			}
		}
		if svc.Port.Name != "" {
			found := false
			for _, p := range obj.Spec.Ports {
				if p.Name == svc.Port.Name {
					found = true
				}
			}
			if !found {
				n("INGRESS_BACKEND_PORT_INVALID", fmt.Sprintf("Service %s has no named port %s", svc.Name, svc.Port.Name), model.SeverityCritical)
			}
		}
		sl := slices[svc.Name]
		total, ready := graph.EndpointReadyCount(sl)
		if total == 0 || ready == 0 {
			n("INGRESS_NO_READY_ENDPOINTS", fmt.Sprintf("backend %s has %d/%d ready endpoints", svc.Name, ready, total), model.SeverityCritical)
		}
	}
	if ing.Spec.DefaultBackend != nil {
		checkBackend(ing.Spec.DefaultBackend.Service)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, p := range rule.HTTP.Paths {
			checkBackend(p.Backend.Service)
		}
	}
	for _, tls := range ing.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}
		if !tlsSecrets[tls.SecretName] {
			n("INGRESS_TLS_SECRET_MISSING", fmt.Sprintf("TLS Secret %s not found", tls.SecretName), model.SeverityCritical)
		}
	}
	sigs = append(sigs, ev...)
	return assemble(cluster, ref, sigs, nil, "full")
}
