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
	"context"
	"fmt"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/model"
)

type Counts struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
}

type ClusterReport struct {
	Status                 string            `json:"status"`
	Cluster                model.ClusterInfo `json:"cluster"`
	Health                 model.Health      `json:"health"`
	Nodes                  Counts            `json:"nodes"`
	Pods                   Counts            `json:"pods"`
	Workloads              Counts            `json:"workloads"`
	Deployments            Counts            `json:"deployments"`
	StatefulSets           Counts            `json:"statefulsets"`
	DaemonSets             Counts            `json:"daemonsets"`
	Jobs                   Counts            `json:"jobs"`
	Storage                Counts            `json:"storage"`
	CriticalIssues         []any             `json:"critical_issues"`
	Warnings               []any             `json:"warnings"`
	HealthyResources       []any             `json:"healthy_resources,omitempty"`
	Visibility             *model.Visibility `json:"visibility"`
	Truncated              bool              `json:"truncated,omitempty"`
	DeepDiagnosisTruncated bool              `json:"deep_diagnosis_truncated,omitempty"`
	DeepDiagnosisSkipped   int               `json:"deep_diagnosis_skipped,omitempty"`
	Message                string            `json:"message"`
}

type Diagnoser func(ctx context.Context, kind, name, namespace string) model.DiagnosticResponse

type inventoryPart struct {
	counts Counts
	health model.Health
	ok     bool
}

func Cluster(ctx context.Context, c *kube.ClusterClient, cfg config.Config, namespace string, includeHealthy bool, maxProblems int, deep Diagnoser) ClusterReport {
	cs := c.Clientset
	vis := model.NewVisibility()
	rep := ClusterReport{
		Status: "success", Cluster: c.Info(), CriticalIssues: []any{}, Warnings: []any{},
		Visibility: vis,
	}
	if maxProblems <= 0 {
		maxProblems = 50
	}
	podNS := namespace
	if podNS == "" {
		podNS = metav1.NamespaceAll
	}
	listOpts := metav1.ListOptions{}

	type cand struct {
		kind, name, ns string
		health         model.Health
		priority       int
		reason         string
	}
	var candidates []cand

	nodePart := inventoryPart{ok: true, health: model.HealthHealthy}
	if nodes, err := cs.CoreV1().Nodes().List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "nodes", "", err)
		nodePart.ok = false
		nodePart.health = model.HealthUnknown
	} else {
		for i := range nodes.Items {
			n := &nodes.Items[i]
			h := FromNode(n)
			nodePart.counts.Total++
			if h == model.HealthHealthy {
				nodePart.counts.Healthy++
				if includeHealthy {
					pushHealthy(&rep, maxProblems, map[string]any{"kind": "Node", "name": n.Name, "health": h})
				}
			} else {
				item := map[string]any{"kind": "Node", "name": n.Name, "health": h}
				pushIssue(&rep, maxProblems, h, item)
				pri := 2
				if h != model.HealthCritical {
					pri = 3
				}
				candidates = append(candidates, cand{kind: "Node", name: n.Name, health: h, priority: pri})
			}
		}
		nodePart.health = partHealth(nodePart.counts, true)
	}
	rep.Nodes = nodePart.counts

	podPart := inventoryPart{ok: true, health: model.HealthHealthy}
	if pods, err := cs.CoreV1().Pods(podNS).List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "pods", namespace, err)
		podPart.ok = false
		podPart.health = model.HealthUnknown
	} else {
		for i := range pods.Items {
			p := &pods.Items[i]
			if p.Status.Phase == corev1.PodSucceeded {
				continue
			}
			h := FromPod(p)
			podPart.counts.Total++
			if h == model.HealthHealthy {
				podPart.counts.Healthy++
				if includeHealthy {
					pushHealthy(&rep, maxProblems, map[string]any{"kind": "Pod", "name": p.Name, "namespace": p.Namespace, "health": h})
				}
			} else {
				item := map[string]any{"kind": "Pod", "name": p.Name, "namespace": p.Namespace, "health": h, "reason": DominantWaiting(p)}
				pushIssue(&rep, maxProblems, h, item)
				pri := 5
				candidates = append(candidates, cand{kind: "Pod", name: p.Name, ns: p.Namespace, health: h, priority: pri, reason: DominantWaiting(p)})
			}
		}
		podPart.health = partHealth(podPart.counts, true)
	}
	rep.Pods = podPart.counts

	scanWL := func(kind string, listErr error, walk func()) inventoryPart {
		p := inventoryPart{ok: true}
		if listErr != nil {
			kube.Record(vis, "list", kind, namespace, listErr)
			p.ok = false
			p.health = model.HealthUnknown
			return p
		}
		walk()
		p.health = partHealth(p.counts, true)
		return p
	}

	var depC Counts
	dpart := scanWL("deployments", nil, func() {})
	if list, err := cs.AppsV1().Deployments(podNS).List(ctx, listOpts); err != nil {
		dpart = scanWL("deployments", err, nil)
	} else {
		dpart = inventoryPart{ok: true}
		for i := range list.Items {
			d := &list.Items[i]
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			h := FromCounts(int(desired), int(d.Status.ReadyReplicas), false)
			dpart.counts.Total++
			if h == model.HealthHealthy {
				dpart.counts.Healthy++
				if includeHealthy {
					pushHealthy(&rep, maxProblems, map[string]any{"kind": "Deployment", "name": d.Name, "namespace": d.Namespace, "health": h})
				}
			} else {
				pushIssue(&rep, maxProblems, h, map[string]any{"kind": "Deployment", "name": d.Name, "namespace": d.Namespace, "health": h, "ready": d.Status.ReadyReplicas, "desired": desired})
				pri := 3
				if h == model.HealthCritical {
					pri = 0
				}
				candidates = append(candidates, cand{kind: "Deployment", name: d.Name, ns: d.Namespace, health: h, priority: pri})
			}
		}
		dpart.health = partHealth(dpart.counts, true)
		depC = dpart.counts
	}
	rep.Deployments = depC
	if !dpart.ok {
		rep.Deployments = Counts{}
	} else {
		rep.Deployments = dpart.counts
	}

	var sp inventoryPart
	if list, err := cs.AppsV1().StatefulSets(podNS).List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "statefulsets", namespace, err)
		sp.ok = false
		sp.health = model.HealthUnknown
	} else {
		sp.ok = true
		for i := range list.Items {
			s := &list.Items[i]
			desired := int32(1)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}
			h := FromCounts(int(desired), int(s.Status.ReadyReplicas), false)
			sp.counts.Total++
			if h == model.HealthHealthy {
				sp.counts.Healthy++
			} else {
				pushIssue(&rep, maxProblems, h, map[string]any{"kind": "StatefulSet", "name": s.Name, "namespace": s.Namespace, "health": h})
				pri := 3
				if h == model.HealthCritical {
					pri = 0
				}
				candidates = append(candidates, cand{kind: "StatefulSet", name: s.Name, ns: s.Namespace, health: h, priority: pri})
			}
		}
		sp.health = partHealth(sp.counts, true)
	}
	rep.StatefulSets = sp.counts

	var dsp inventoryPart
	if list, err := cs.AppsV1().DaemonSets(podNS).List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "daemonsets", namespace, err)
		dsp.ok = false
		dsp.health = model.HealthUnknown
	} else {
		dsp.ok = true
		for i := range list.Items {
			d := &list.Items[i]
			h := FromCounts(int(d.Status.DesiredNumberScheduled), int(d.Status.NumberReady), false)
			dsp.counts.Total++
			if h == model.HealthHealthy {
				dsp.counts.Healthy++
			} else {
				pushIssue(&rep, maxProblems, h, map[string]any{"kind": "DaemonSet", "name": d.Name, "namespace": d.Namespace, "health": h})
				pri := 3
				if h == model.HealthCritical {
					pri = 0
				}
				candidates = append(candidates, cand{kind: "DaemonSet", name: d.Name, ns: d.Namespace, health: h, priority: pri})
			}
		}
		dsp.health = partHealth(dsp.counts, true)
	}
	rep.DaemonSets = dsp.counts

	var jp inventoryPart
	if list, err := cs.BatchV1().Jobs(podNS).List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "jobs", namespace, err)
		jp.ok = false
		jp.health = model.HealthUnknown
	} else {
		jp.ok = true
		for i := range list.Items {
			j := &list.Items[i]
			h := jobHealth(j)
			jp.counts.Total++
			if h == model.HealthHealthy {
				jp.counts.Healthy++
			} else {
				pushIssue(&rep, maxProblems, h, map[string]any{"kind": "Job", "name": j.Name, "namespace": j.Namespace, "health": h})
				pri := 1
				if h != model.HealthCritical {
					pri = 3
				}
				candidates = append(candidates, cand{kind: "Job", name: j.Name, ns: j.Namespace, health: h, priority: pri})
			}
		}
		jp.health = partHealth(jp.counts, true)
	}
	rep.Jobs = jp.counts

	var st inventoryPart
	if list, err := cs.CoreV1().PersistentVolumeClaims(podNS).List(ctx, listOpts); err != nil {
		kube.Record(vis, "list", "persistentvolumeclaims", namespace, err)
		st.ok = false
		st.health = model.HealthUnknown
	} else {
		st.ok = true
		for i := range list.Items {
			p := &list.Items[i]
			st.counts.Total++
			h := model.HealthHealthy
			if p.Status.Phase != corev1.ClaimBound {
				h = model.HealthCritical
				pushIssue(&rep, maxProblems, h, map[string]any{"kind": "PVC", "name": p.Name, "namespace": p.Namespace, "phase": p.Status.Phase, "health": h})
				candidates = append(candidates, cand{kind: "PersistentVolumeClaim", name: p.Name, ns: p.Namespace, health: h, priority: 4})
			} else {
				st.counts.Healthy++
			}
		}
		st.health = partHealth(st.counts, true)
	}
	rep.Storage = st.counts

	rep.Workloads.Total = rep.Deployments.Total + rep.StatefulSets.Total + rep.DaemonSets.Total + rep.Jobs.Total
	rep.Workloads.Healthy = rep.Deployments.Healthy + rep.StatefulSets.Healthy + rep.DaemonSets.Healthy + rep.Jobs.Healthy
	wlOK := dpart.ok && sp.ok && dsp.ok && jp.ok
	wlHealth := partHealth(rep.Workloads, wlOK)

	parts := []model.Health{nodePart.health, podPart.health, wlHealth, st.health}
	rep.Health = Combine(parts...)

	maxDeep := cfg.MaxClusterDeepDiagnoses
	if maxDeep <= 0 {
		maxDeep = 8
	}
	if deep != nil && len(candidates) > 0 {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].priority != candidates[j].priority {
				return candidates[i].priority < candidates[j].priority
			}
			if candidates[i].kind != candidates[j].kind {
				return candidates[i].kind < candidates[j].kind
			}
			if candidates[i].ns != candidates[j].ns {
				return candidates[i].ns < candidates[j].ns
			}
			return candidates[i].name < candidates[j].name
		})
		if len(candidates) > maxDeep {
			rep.DeepDiagnosisTruncated = true
			rep.DeepDiagnosisSkipped = len(candidates) - maxDeep
			candidates = candidates[:maxDeep]
		}
		sem := cfg.MaxConcurrentK8s
		if sem <= 0 {
			sem = 4
		}
		type slot struct{}
		gate := make(chan slot, sem)
		type deepRes struct {
			kind, name, ns string
			health         model.Health
			cat            string
			conf           model.Confidence
		}
		ch := make(chan deepRes, len(candidates))
		for _, cnd := range candidates {
			cnd := cnd
			gate <- slot{}
			go func() {
				defer func() { <-gate }()
				if ctx.Err() != nil {
					ch <- deepRes{kind: cnd.kind, name: cnd.name, ns: cnd.ns, health: cnd.health}
					return
				}
				out := deep(ctx, cnd.kind, cnd.name, cnd.ns)
				dr := deepRes{kind: cnd.kind, name: cnd.name, ns: cnd.ns, health: out.Health}
				if out.RootCause != nil {
					dr.cat = out.RootCause.Category
					dr.conf = out.RootCause.Confidence
				}
				ch <- dr
			}()
		}
		enriched := map[string]deepRes{}
		for range candidates {
			r := <-ch
			enriched[r.kind+"/"+r.ns+"/"+r.name] = r
		}
		for i, item := range rep.CriticalIssues {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s/%s/%s", m["kind"], nsOf(m), m["name"])
			if d, ok := enriched[key]; ok && d.cat != "" {
				m["root_cause_category"] = d.cat
				m["confidence"] = d.conf
				m["impact"] = d.health
				rep.CriticalIssues[i] = m
			}
		}
	} else if deep != nil && len(candidates) > maxDeep {
		rep.DeepDiagnosisTruncated = true
	}

	if vis.Limited {
		rep.Status = "partial"
	}
	rep.Message = fmt.Sprintf("cluster health=%s critical=%d warnings=%d", rep.Health, len(rep.CriticalIssues), len(rep.Warnings))
	if includeHealthy {
		rep.Message = fmt.Sprintf("cluster health=%s critical=%d warnings=%d healthy_listed=%d", rep.Health, len(rep.CriticalIssues), len(rep.Warnings), len(rep.HealthyResources))
	}
	return rep
}

func nsOf(m map[string]any) string {
	if v, ok := m["namespace"].(string); ok {
		return v
	}
	return ""
}

func partHealth(c Counts, listedOK bool) model.Health {
	if !listedOK {
		return model.HealthUnknown
	}
	if c.Total == 0 {
		return model.HealthHealthy
	}
	if c.Healthy == 0 {
		return model.HealthCritical
	}
	if c.Healthy < c.Total {
		return model.HealthDegraded
	}
	return model.HealthHealthy
}

func pushIssue(rep *ClusterReport, max int, h model.Health, item any) {
	if len(rep.CriticalIssues)+len(rep.Warnings) >= max {
		rep.Truncated = true
		return
	}
	if h == model.HealthCritical {
		rep.CriticalIssues = append(rep.CriticalIssues, item)
	} else {
		rep.Warnings = append(rep.Warnings, item)
	}
}

func pushHealthy(rep *ClusterReport, max int, item any) {
	if len(rep.HealthyResources) >= max {
		rep.Truncated = true
		return
	}
	rep.HealthyResources = append(rep.HealthyResources, item)
}

func jobHealth(j *batchv1.Job) model.Health {
	for _, cnd := range j.Status.Conditions {
		if cnd.Type == batchv1.JobFailed && cnd.Status == corev1.ConditionTrue {
			return model.HealthCritical
		}
	}
	if j.Status.Succeeded == 0 && j.Status.Failed > 0 {
		return model.HealthCritical
	}
	return model.HealthHealthy
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
