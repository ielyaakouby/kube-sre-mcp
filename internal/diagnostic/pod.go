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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ielyaakouby/kube-sre-mcp/internal/graph"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/security"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

type PodInput struct {
	Pod                    *corev1.Pod
	Events                 []signal.DiagnosticSignal
	LogSignals             []signal.DiagnosticSignal
	Node                   *corev1.Node
	CandidateNodes         []*corev1.Node
	CandidatesListed       bool
	UnsupportedConstraints []string
	ConfigMaps             map[string]*corev1.ConfigMap
	ConfigMapUnknown       map[string]bool
	SecretKeys             map[string][]string
	SecretsExist           map[string]bool
	SecretUnknown          map[string]bool
	PVCs                   map[string]*corev1.PersistentVolumeClaim
	PVCUnknown             map[string]bool
	ServiceAccount         *corev1.ServiceAccount
	SAUnknown              bool
	MetricsAvail           bool
	HighMemory             bool
	Visibility             *model.Visibility
}

func DiagnosePod(cluster model.ClusterInfo, in PodInput) model.DiagnosticResponse {
	p := in.Pod
	ref := graph.PodRef(p)
	var sigs []signal.DiagnosticSignal
	id := 1
	next := func() string { s := fmt.Sprintf("E-STATUS-%d", id); id++; return s }

	phase := string(p.Status.Phase)
	all := append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...)
	all = append(all, p.Status.ContainerStatuses...)

	for _, cs := range all {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", w.Reason, model.SeverityCritical, model.ConfidenceConfirmed, ref,
				fmt.Sprintf("container %s waiting: %s – %s", cs.Name, w.Reason, security.Redact(w.Message))))
		}
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			reason := t.Reason
			if reason == "" {
				reason = "Error"
			}
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", reason, model.SeverityCritical, model.ConfidenceConfirmed, ref,
				fmt.Sprintf("container %s terminated: %s (exit %d)", cs.Name, reason, t.ExitCode)))
		}
		if ls := cs.LastTerminationState.Terminated; ls != nil && ls.Reason == "OOMKilled" {
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Memory", "OOMKilled", model.SeverityCritical, model.ConfidenceConfirmed, ref,
				fmt.Sprintf("container %s last state OOMKilled (exit %d)", cs.Name, ls.ExitCode)))
		}
		if cs.RestartCount > 0 {
			sev := model.SeverityWarning
			if cs.RestartCount > 5 {
				sev = model.SeverityCritical
			}
			sigs = append(sigs, signal.New(next(), signal.SourceStatus, "Container", "Restarts", sev, model.ConfidenceHigh, ref,
				fmt.Sprintf("container %s restartCount=%d", cs.Name, cs.RestartCount)))
		}
	}

	for _, c := range p.Status.Conditions {
		if c.Status == corev1.ConditionFalse {
			sigs = append(sigs, signal.New(next(), signal.SourceCondition, "Pod", string(c.Type), model.SeverityWarning, model.ConfidenceHigh, ref,
				fmt.Sprintf("condition %s=False: %s", c.Type, c.Message)))
		}
	}

	sigs = append(sigs, probeSignals(p, ref, &id)...)
	sched, mode := scheduleSignals(p, in.Node, in.CandidateNodes, in.CandidatesListed, in.UnsupportedConstraints, ref, &id)
	sigs = append(sigs, sched...)
	sigs = append(sigs, depSignals(p, in, ref, &id)...)
	if in.Node != nil {
		sigs = append(sigs, nodeConditionSignals(in.Node, ref, &id)...)
		if in.Node.Spec.Unschedulable {
			sigs = append(sigs, signal.New(next(), signal.SourceScheduler, "Node", "unschedulable", model.SeverityWarning, model.ConfidenceHigh, ref,
				fmt.Sprintf("scheduled/target node %s is cordoned", in.Node.Name)))
		}
	}
	if in.HighMemory {
		sigs = append(sigs, signal.New(next(), signal.SourceMetric, "Memory", "high_memory", model.SeverityWarning, model.ConfidenceLow, ref,
			"current memory usage is high versus container limit (supporting evidence only; not a leak or future OOM prediction)"))
	}
	sigs = append(sigs, in.Events...)
	sigs = append(sigs, in.LogSignals...)

	resp := assemble(cluster, ref, sigs, in.Visibility, "full")
	resp.ScheduleMode = mode
	if mode != "" || len(in.UnsupportedConstraints) > 0 {
		unsupported := []string{}
		for _, u := range in.UnsupportedConstraints {
			if strings.Contains(strings.ToLower(u), "anti-affinity") {
				unsupported = append(unsupported, "podAntiAffinity")
			}
			if strings.Contains(strings.ToLower(u), "topologyspread") {
				unsupported = append(unsupported, "topologySpreadConstraints")
			}
		}
		resp.SchedulerAnalysis = &model.SchedulerAnalysis{
			Completeness: "partial",
			Mode:         mode,
			Unsupported:  uniqueStrings(unsupported),
		}
	}
	if phase == "Running" && resp.Health == model.HealthHealthy {
		resp.Summary = fmt.Sprintf("Pod %s appears healthy and running.", p.Name)
	} else if resp.Summary == "" {
		resp.Summary = fmt.Sprintf("Pod %s is in %s state.", p.Name, phase)
	}
	return resp
}

func probeSignals(p *corev1.Pod, ref model.ResourceRef, id *int) []signal.DiagnosticSignal {
	next := func() string { s := fmt.Sprintf("E-PROBE-%d", *id); *id++; return s }
	var out []signal.DiagnosticSignal
	ready := map[string]bool{}
	for _, cs := range p.Status.ContainerStatuses {
		ready[cs.Name] = cs.Ready
	}
	check := func(c corev1.Container, probe *corev1.Probe, kind string) {
		if probe == nil {
			return
		}
		pt := probeType(probe)
		if kind == "readiness" && p.Status.Phase == corev1.PodRunning && ready[c.Name] == false {
			out = append(out, signal.New(next(), signal.SourceStatus, "Probe", "READINESS_PROBE_FAILED", model.SeverityWarning, model.ConfidenceHigh, ref,
				fmt.Sprintf("container %s is running but not ready; readinessProbe configured (%s)", c.Name, pt)))
		}
	}
	unhealthyHint := strings.ToLower(strings.Join(func() []string {
		var m []string
		for _, c := range p.Status.Conditions {
			m = append(m, c.Message+" "+c.Reason)
		}
		return m
	}(), " "))
	for _, c := range p.Spec.Containers {
		check(c, c.ReadinessProbe, "readiness")
		if c.LivenessProbe != nil && strings.Contains(unhealthyHint, "liveness") {
			out = append(out, signal.New(next(), signal.SourceStatus, "Probe", "LIVENESS_PROBE_FAILED", model.SeverityWarning, model.ConfidenceHigh, ref,
				fmt.Sprintf("container %s livenessProbe failed (%s)", c.Name, probeType(c.LivenessProbe))))
		}
		if c.StartupProbe != nil && strings.Contains(unhealthyHint, "startup") {
			out = append(out, signal.New(next(), signal.SourceStatus, "Probe", "STARTUP_PROBE_FAILED", model.SeverityWarning, model.ConfidenceHigh, ref,
				fmt.Sprintf("container %s startupProbe failed (%s)", c.Name, probeType(c.StartupProbe))))
		}
	}
	return out
}

func probeType(p *corev1.Probe) string {
	if p.HTTPGet != nil {
		return "http"
	}
	if p.TCPSocket != nil {
		return "tcp"
	}
	if p.Exec != nil {
		return "exec"
	}
	if p.GRPC != nil {
		return "grpc"
	}
	return "unknown"
}

func scheduleSignals(p *corev1.Pod, assigned *corev1.Node, candidates []*corev1.Node, listed bool, unsupported []string, ref model.ResourceRef, id *int) ([]signal.DiagnosticSignal, string) {
	if p.Status.Phase != corev1.PodPending || p.Spec.NodeName != "" {
		return nil, ""
	}
	next := func(reason, msg string, conf model.Confidence) signal.DiagnosticSignal {
		s := fmt.Sprintf("E-SCHED-%d", *id)
		*id++
		return signal.New(s, signal.SourceScheduler, "Scheduler", reason, model.SeverityCritical, conf, ref, msg)
	}
	var out []signal.DiagnosticSignal
	mode := "approximate_eligibility"
	for _, u := range unsupported {
		out = append(out, next("UNSUPPORTED_SCHEDULER_CONSTRAINT", u, model.ConfidenceLow))
		mode = "approximate_eligibility_partial"
	}
	nodes := candidates
	if len(nodes) == 0 && assigned != nil {
		nodes = []*corev1.Node{assigned}
		listed = true
	}
	if !listed {
		out = append(out, next("NO_ELIGIBLE_NODE", "Pod is Pending with no assigned node; candidate node list was not available so scheduling RCA is events-only (not a computed eligibility analysis)", model.ConfidenceLow))
		return out, "events_only"
	}
	reqCPU := resource.MustParse("0")
	reqMem := resource.MustParse("0")
	add := func(list []corev1.Container) {
		for _, c := range list {
			reqCPU.Add(c.Resources.Requests[corev1.ResourceCPU])
			reqMem.Add(c.Resources.Requests[corev1.ResourceMemory])
		}
	}
	add(p.Spec.Containers)
	add(p.Spec.InitContainers)

	reasons := map[string]int{}
	eligible := 0
	for _, node := range nodes {
		if node == nil {
			continue
		}
		blocked := false
		ready := true
		for _, c := range node.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionFalse {
				ready = false
			}
		}
		if !ready {
			reasons["NO_ELIGIBLE_NODE"]++
			blocked = true
		}
		if node.Spec.Unschedulable {
			reasons["NO_ELIGIBLE_NODE"]++
			blocked = true
		}
		allocCPU := node.Status.Allocatable[corev1.ResourceCPU]
		allocMem := node.Status.Allocatable[corev1.ResourceMemory]
		if reqCPU.Cmp(allocCPU) > 0 {
			reasons["INSUFFICIENT_CPU"]++
			blocked = true
		}
		if reqMem.Cmp(allocMem) > 0 {
			reasons["INSUFFICIENT_MEMORY"]++
			blocked = true
		}
		if p.Spec.NodeSelector != nil && !labelsMatch(p.Spec.NodeSelector, node.Labels) {
			reasons["NODE_SELECTOR_MISMATCH"]++
			blocked = true
		}
		if aff := p.Spec.Affinity; aff != nil && aff.NodeAffinity != nil && aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			ok, unsup := nodeAffinityMatches(aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution, node)
			if unsup {
				reasons["UNSUPPORTED_SCHEDULER_CONSTRAINT"]++
				blocked = true
			} else if !ok {
				reasons["NODE_AFFINITY_MISMATCH"]++
				blocked = true
			}
		}
		if taintBlocks(node.Spec.Taints, p.Spec.Tolerations) {
			reasons["TAINT_NOT_TOLERATED"]++
			blocked = true
		}
		if !blocked {
			eligible++
		}
	}
	if eligible == 0 {
		best, n, bestPri := "", 0, -1
		pri := func(k string) int {
			switch k {
			case "INSUFFICIENT_CPU", "INSUFFICIENT_MEMORY", "TAINT_NOT_TOLERATED", "NODE_SELECTOR_MISMATCH", "NODE_AFFINITY_MISMATCH":
				return 2
			case "UNSUPPORTED_SCHEDULER_CONSTRAINT":
				return 1
			default:
				return 0
			}
		}
		for k, v := range reasons {
			p := pri(k)
			if p > bestPri || (p == bestPri && (v > n || (v == n && (best == "" || k < best)))) {
				best, n, bestPri = k, v, p
			}
		}
		if best == "" {
			best = "NO_ELIGIBLE_NODE"
		}
		msg := fmt.Sprintf("approximate eligibility: 0/%d candidate nodes eligible; dominant reason %s (%d nodes)", len(nodes), best, n)
		out = append(out, next(best, msg, model.ConfidenceHigh))
	}
	return out, mode
}

func labelsMatch(sel, labels map[string]string) bool {
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func nodeAffinityMatches(req *corev1.NodeSelector, node *corev1.Node) (ok bool, unsupported bool) {
	for _, term := range req.NodeSelectorTerms {
		termOK := true
		for _, expr := range term.MatchExpressions {
			lv := node.Labels[expr.Key]
			switch expr.Operator {
			case corev1.NodeSelectorOpIn:
				if !containsStr(expr.Values, lv) {
					termOK = false
				}
			case corev1.NodeSelectorOpNotIn:
				if containsStr(expr.Values, lv) {
					termOK = false
				}
			case corev1.NodeSelectorOpExists:
				if _, exists := node.Labels[expr.Key]; !exists {
					termOK = false
				}
			case corev1.NodeSelectorOpDoesNotExist:
				if _, exists := node.Labels[expr.Key]; exists {
					termOK = false
				}
			case corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt:
				termOK = false
			}
		}
		for _, f := range term.MatchFields {
			if f.Key == "metadata.name" && f.Operator == corev1.NodeSelectorOpIn {
				if !containsStr(f.Values, node.Name) {
					termOK = false
				}
			} else {
				unsupported = true
				termOK = false
			}
		}
		if termOK {
			return true, unsupported
		}
	}
	return false, unsupported
}

func taintBlocks(taints []corev1.Taint, tols []corev1.Toleration) bool {
	for _, t := range taints {
		if t.Effect != corev1.TaintEffectNoSchedule && t.Effect != corev1.TaintEffectNoExecute {
			continue
		}
		matched := false
		for _, tol := range tols {
			if tolerates(tol, t) {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func tolerates(tol corev1.Toleration, t corev1.Taint) bool {
	if tol.Key != "" && tol.Key != t.Key {
		return false
	}
	if tol.Effect != "" && tol.Effect != t.Effect {
		return false
	}
	switch tol.Operator {
	case corev1.TolerationOpExists, "":
		return tol.Key == "" || tol.Key == t.Key
	case corev1.TolerationOpEqual:
		return tol.Value == t.Value
	}
	return false
}

func containsStr(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func depSignals(p *corev1.Pod, in PodInput, ref model.ResourceRef, id *int) []signal.DiagnosticSignal {
	next := func(reason, msg string) signal.DiagnosticSignal {
		s := fmt.Sprintf("E-DEP-%d", *id)
		*id++
		return signal.New(s, signal.SourceDependency, "Dependency", reason, model.SeverityCritical, model.ConfidenceHigh, ref, msg)
	}
	var out []signal.DiagnosticSignal
	seenCM := map[string]struct{}{}
	seenSec := map[string]struct{}{}
	addCM := func(name, key string) {
		if name == "" {
			return
		}
		seenCM[name] = struct{}{}
		cm, ok := in.ConfigMaps[name]
		if !ok {
			if in.ConfigMapUnknown[name] {
				return
			}
			out = append(out, next("MISSING_CONFIGMAP", fmt.Sprintf("ConfigMap %s not found", name)))
			return
		}
		if key != "" {
			if _, exists := cm.Data[key]; !exists {
				if _, exists2 := cm.BinaryData[key]; !exists2 {
					out = append(out, next("MISSING_CONFIGMAP", fmt.Sprintf("ConfigMap %s missing key %s", name, key)))
				}
			}
		}
	}
	addSec := func(name, key string) {
		if name == "" {
			return
		}
		seenSec[name] = struct{}{}
		if in.SecretUnknown[name] {
			return
		}
		exists, known := in.SecretsExist[name]
		if !known {
			return
		}
		if !exists {
			out = append(out, next("MISSING_SECRET", fmt.Sprintf("Secret %s not found", name)))
			return
		}
		if key != "" && in.SecretKeys != nil {
			if !containsStr(in.SecretKeys[name], key) {
				out = append(out, next("MISSING_SECRET", fmt.Sprintf("Secret %s missing key %s", name, key)))
			}
		}
	}
	walk := func(c corev1.Container) {
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				addCM(e.ValueFrom.ConfigMapKeyRef.Name, e.ValueFrom.ConfigMapKeyRef.Key)
			}
			if e.ValueFrom.SecretKeyRef != nil {
				addSec(e.ValueFrom.SecretKeyRef.Name, e.ValueFrom.SecretKeyRef.Key)
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				addCM(ef.ConfigMapRef.Name, "")
			}
			if ef.SecretRef != nil {
				addSec(ef.SecretRef.Name, "")
			}
		}
	}
	for _, c := range p.Spec.Containers {
		walk(c)
	}
	for _, c := range p.Spec.InitContainers {
		walk(c)
	}
	for _, v := range p.Spec.Volumes {
		if v.ConfigMap != nil {
			addCM(v.ConfigMap.Name, "")
		}
		if v.Secret != nil {
			addSec(v.Secret.SecretName, "")
		}
		if v.PersistentVolumeClaim != nil {
			pvc, ok := in.PVCs[v.PersistentVolumeClaim.ClaimName]
			if in.PVCUnknown[v.PersistentVolumeClaim.ClaimName] {
				continue
			}
			if !ok {
				out = append(out, next("PVC_NOT_FOUND", fmt.Sprintf("PVC %s not found", v.PersistentVolumeClaim.ClaimName)))
			} else if pvc.Status.Phase != corev1.ClaimBound {
				out = append(out, next("PVC_PENDING", fmt.Sprintf("PVC %s phase=%s", pvc.Name, pvc.Status.Phase)))
			}
		}
	}
	if p.Spec.ServiceAccountName != "" && in.ServiceAccount == nil && !in.SAUnknown {
		out = append(out, next("MISSING_SERVICE_ACCOUNT", fmt.Sprintf("ServiceAccount %s not found", p.Spec.ServiceAccountName)))
	}
	for _, s := range p.Spec.ImagePullSecrets {
		if in.SecretUnknown[s.Name] {
			continue
		}
		if exists, known := in.SecretsExist[s.Name]; known && !exists {
			out = append(out, next("MISSING_SECRET", fmt.Sprintf("imagePullSecret %s not found", s.Name)))
		}
	}
	_ = seenCM
	_ = seenSec
	return out
}

func nodeConditionSignals(n *corev1.Node, ref model.ResourceRef, id *int) []signal.DiagnosticSignal {
	var out []signal.DiagnosticSignal
	for _, c := range n.Status.Conditions {
		healthy := (c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue) ||
			(c.Type != corev1.NodeReady && c.Status == corev1.ConditionFalse)
		if healthy {
			continue
		}
		s := fmt.Sprintf("E-NODE-%d", *id)
		*id++
		out = append(out, signal.New(s, signal.SourceCondition, "Node", string(c.Type), model.SeverityCritical, model.ConfidenceConfirmed, ref,
			fmt.Sprintf("node %s condition %s=%s: %s", n.Name, c.Type, c.Status, c.Message)))
	}
	return out
}

func UnhealthyPod(p *corev1.Pod) bool {
	if p.Status.Phase == corev1.PodSucceeded {
		return false
	}
	if p.Status.Phase != corev1.PodRunning {
		return true
	}
	for _, cs := range p.Status.ContainerStatuses {
		if !cs.Ready || cs.RestartCount > 5 {
			return true
		}
		if cs.State.Waiting != nil {
			return true
		}
	}
	return false
}

func DominantWaiting(p *corev1.Pod) string {
	all := append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...)
	all = append(all, p.Status.ContainerStatuses...)
	for _, cs := range all {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return cs.State.Terminated.Reason
		}
	}
	return string(p.Status.Phase)
}

func waitingMessage(p *corev1.Pod) string {
	all := append([]corev1.ContainerStatus{}, p.Status.InitContainerStatuses...)
	all = append(all, p.Status.ContainerStatuses...)
	for _, cs := range all {
		if cs.State.Waiting != nil {
			return strings.ToLower(cs.State.Waiting.Message + " " + cs.State.Waiting.Reason)
		}
	}
	return ""
}
