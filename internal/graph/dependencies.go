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

package graph

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"kube-sre-mcp/internal/model"
)

type Builder struct {
	CS     kubernetes.Interface
	Limits Limits
}

func (b *Builder) fromPodSpec(g *Graph, ns string, owner model.ResourceRef, spec corev1.PodSpec) {
	if spec.NodeName != "" {
		g.AddEdge(ScheduledOn, owner, model.ResourceRef{APIVersion: "v1", Kind: "Node", Name: spec.NodeName})
	}
	if spec.ServiceAccountName != "" {
		g.AddEdge(UsesServiceAccount, owner, model.ResourceRef{APIVersion: "v1", Kind: "ServiceAccount", Namespace: ns, Name: spec.ServiceAccountName})
	}
	for _, s := range spec.ImagePullSecrets {
		g.AddEdge(UsesImagePullSecret, owner, model.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: ns, Name: s.Name})
	}
	vols := spec.Volumes
	add := func(c corev1.Container) {
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				g.AddEdge(UsesConfigMap, owner, model.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: ns, Name: e.ValueFrom.ConfigMapKeyRef.Name})
			}
			if e.ValueFrom.SecretKeyRef != nil {
				g.AddEdge(UsesSecret, owner, model.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: ns, Name: e.ValueFrom.SecretKeyRef.Name})
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				g.AddEdge(UsesConfigMap, owner, model.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: ns, Name: ef.ConfigMapRef.Name})
			}
			if ef.SecretRef != nil {
				g.AddEdge(UsesSecret, owner, model.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: ns, Name: ef.SecretRef.Name})
			}
		}
	}
	for _, c := range spec.Containers {
		add(c)
	}
	for _, c := range spec.InitContainers {
		add(c)
	}
	for _, v := range vols {
		if v.ConfigMap != nil {
			g.AddEdge(UsesConfigMap, owner, model.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: ns, Name: v.ConfigMap.Name})
		}
		if v.Secret != nil {
			g.AddEdge(UsesSecret, owner, model.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: ns, Name: v.Secret.SecretName})
		}
		if v.PersistentVolumeClaim != nil {
			g.AddEdge(UsesPVC, owner, model.ResourceRef{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: ns, Name: v.PersistentVolumeClaim.ClaimName})
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.Secret != nil && src.Secret.Name != "" {
					g.AddEdge(UsesSecret, owner, model.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: ns, Name: src.Secret.Name})
				}
				if src.ConfigMap != nil && src.ConfigMap.Name != "" {
					g.AddEdge(UsesConfigMap, owner, model.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: ns, Name: src.ConfigMap.Name})
				}
				if src.ServiceAccountToken != nil {
					g.AddEdge(UsesServiceAccount, owner, model.ResourceRef{APIVersion: "v1", Kind: "ServiceAccount", Namespace: ns, Name: spec.ServiceAccountName})
				}
			}
		}
	}
}

func capReached(g *Graph, lim Limits) bool {
	return capReachedAt(g, lim, 0)
}

func capReachedAt(g *Graph, lim Limits, depth int) bool {
	if lim.MaxDepth > 0 && depth > lim.MaxDepth {
		g.Truncated = true
		if g.TruncationReason == "" {
			g.TruncationReason = "max_depth"
		}
		return true
	}
	if lim.MaxNodes > 0 && len(g.Nodes) >= lim.MaxNodes {
		g.Truncated = true
		if g.TruncationReason == "" {
			g.TruncationReason = "max_nodes"
		}
		return true
	}
	return false
}

func (b *Builder) Deployment(ctx context.Context, d *appsv1.Deployment) (*Graph, error) {
	g := New(DeployRef(d))
	ns := d.Namespace
	rss, err := b.CS.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return g, err
	}
	for i := range rss.Items {
		rs := &rss.Items[i]
		if !OwnedByUID(rs, d.UID) {
			continue
		}
		if capReachedAt(g, b.Limits, 1) {
			return g, nil
		}
		rsRef := model.ResourceRef{APIVersion: "apps/v1", Kind: "ReplicaSet", Namespace: ns, Name: rs.Name, UID: rs.UID}
		g.AddEdge(Owns, DeployRef(d), rsRef)
		g.AddEdge(OwnedBy, rsRef, DeployRef(d))
		if err := b.addPodsByOwner(ctx, g, ns, rs.UID, rsRef, rs.Spec.Selector, 2); err != nil {
			return g, err
		}
	}
	b.fromPodSpec(g, ns, DeployRef(d), d.Spec.Template.Spec)
	_ = b.linkHPA(ctx, g, ns, "Deployment", d.Name)
	if err := b.linkPDB(ctx, g, ns, d.Spec.Selector); err != nil {
		return g, err
	}
	return g, nil
}

func (b *Builder) addPodsByOwner(ctx context.Context, g *Graph, ns string, uid types.UID, owner model.ResourceRef, sel *metav1.LabelSelector, depth int) error {
	opts := metav1.ListOptions{}
	if sel != nil {
		opts.LabelSelector = metav1.FormatLabelSelector(sel)
	}
	pods, err := b.CS.CoreV1().Pods(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if !OwnedByUID(p, uid) {
			continue
		}
		if capReachedAt(g, b.Limits, depth) {
			return nil
		}
		pr := PodRef(p)
		g.AddEdge(Owns, owner, pr)
		g.AddEdge(OwnedBy, pr, owner)
		b.fromPodSpec(g, ns, pr, p.Spec)
	}
	return nil
}

func (b *Builder) StatefulSet(ctx context.Context, s *appsv1.StatefulSet) (*Graph, error) {
	g := New(STSRef(s))
	if err := b.addPodsByOwner(ctx, g, s.Namespace, s.UID, STSRef(s), s.Spec.Selector, 1); err != nil {
		return g, err
	}
	b.fromPodSpec(g, s.Namespace, STSRef(s), s.Spec.Template.Spec)
	pvcs, err := b.CS.CoreV1().PersistentVolumeClaims(s.Namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range pvcs.Items {
			pvc := &pvcs.Items[i]
			if !STSOwnsPVC(s, pvc) {
				continue
			}
			pref := model.ResourceRef{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: pvc.Namespace, Name: pvc.Name, UID: pvc.UID}
			g.AddEdge(UsesPVC, STSRef(s), pref)
			if pvc.Spec.VolumeName != "" {
				g.AddEdge(BackedByPV, pref, model.ResourceRef{APIVersion: "v1", Kind: "PersistentVolume", Name: pvc.Spec.VolumeName})
			}
			if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
				g.AddEdge(UsesStorageClass, pref, model.ResourceRef{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: *pvc.Spec.StorageClassName})
			}
		}
	}
	_ = b.linkHPA(ctx, g, s.Namespace, "StatefulSet", s.Name)
	return g, nil
}

func (b *Builder) DaemonSet(ctx context.Context, d *appsv1.DaemonSet) (*Graph, error) {
	g := New(DSRef(d))
	if err := b.addPodsByOwner(ctx, g, d.Namespace, d.UID, DSRef(d), d.Spec.Selector, 1); err != nil {
		return g, err
	}
	b.fromPodSpec(g, d.Namespace, DSRef(d), d.Spec.Template.Spec)
	return g, nil
}

func (b *Builder) Job(ctx context.Context, j *batchv1.Job) (*Graph, error) {
	g := New(JobRef(j))
	if err := b.addPodsByOwner(ctx, g, j.Namespace, j.UID, JobRef(j), nil, 1); err != nil {
		return g, err
	}
	return g, nil
}

func (b *Builder) CronJob(ctx context.Context, c *batchv1.CronJob) (*Graph, error) {
	g := New(CronRef(c))
	jobs, err := b.CS.BatchV1().Jobs(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return g, err
	}
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if !OwnedByUID(j, c.UID) {
			continue
		}
		jr := JobRef(j)
		g.AddEdge(Owns, CronRef(c), jr)
		if err := b.addPodsByOwner(ctx, g, c.Namespace, j.UID, jr, nil, 2); err != nil {
			return g, err
		}
	}
	return g, nil
}

func (b *Builder) Pod(ctx context.Context, p *corev1.Pod) (*Graph, error) {
	g := New(PodRef(p))
	b.fromPodSpec(g, p.Namespace, PodRef(p), p.Spec)
	return g, nil
}

func (b *Builder) Service(ctx context.Context, svc *corev1.Service) (*Graph, error) {
	g := New(model.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: svc.Namespace, Name: svc.Name, UID: svc.UID})
	sel := svc.Spec.Selector
	if len(sel) == 0 {
		return g, nil
	}
	pods, err := b.CS.CoreV1().Pods(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: SelectorString(sel)})
	if err != nil {
		return g, err
	}
	src := model.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: svc.Namespace, Name: svc.Name, UID: svc.UID}
	for i := range pods.Items {
		p := &pods.Items[i]
		pr := PodRef(p)
		g.AddEdge(Selects, src, pr)
		g.AddEdge(ExposedByService, pr, src)
		b.fromPodSpec(g, p.Namespace, pr, p.Spec)
	}
	slices, err := b.CS.DiscoveryV1().EndpointSlices(svc.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "kubernetes.io/service-name=" + svc.Name,
	})
	if err != nil {
		return g, err
	}
	for i := range slices.Items {
		s := &slices.Items[i]
		g.AddEdge(HasEndpoint, src, model.ResourceRef{APIVersion: "discovery.k8s.io/v1", Kind: "EndpointSlice", Namespace: s.Namespace, Name: s.Name, UID: s.UID})
	}
	return g, nil
}

func (b *Builder) Ingress(ctx context.Context, ing *networkingv1.Ingress) (*Graph, error) {
	g := New(model.ResourceRef{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Namespace: ing.Namespace, Name: ing.Name, UID: ing.UID})
	src := g.Root
	addSvc := func(name string) {
		if name == "" {
			return
		}
		g.AddEdge(RoutedByIngress, model.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: ing.Namespace, Name: name}, src)
	}
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		addSvc(ing.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, p := range rule.HTTP.Paths {
			if p.Backend.Service != nil {
				addSvc(p.Backend.Service.Name)
			}
		}
	}
	return g, nil
}

func (b *Builder) linkHPA(ctx context.Context, g *Graph, ns, kind, name string) error {
	hpas, err := b.CS.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range hpas.Items {
		h := &hpas.Items[i]
		if h.Spec.ScaleTargetRef.Kind == kind && h.Spec.ScaleTargetRef.Name == name {
			g.AddEdge(ScaledByHPA, g.Root, model.ResourceRef{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler", Namespace: ns, Name: h.Name, UID: h.UID})
		}
	}
	return nil
}

func (b *Builder) linkPDB(ctx context.Context, g *Graph, ns string, sel *metav1.LabelSelector) error {
	if sel == nil {
		return nil
	}
	pdbs, err := b.CS.PolicyV1().PodDisruptionBudgets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range pdbs.Items {
		p := &pdbs.Items[i]
		if p.Spec.Selector == nil {
			continue
		}
		if !MatchLabelSelector(p.Spec.Selector, sel.MatchLabels) {
			continue
		}
		g.AddEdge(ProtectedByPDB, g.Root, model.ResourceRef{APIVersion: "policy/v1", Kind: "PodDisruptionBudget", Namespace: ns, Name: p.Name, UID: p.UID})
	}
	return nil
}

func matchOverlap(a, b *metav1.LabelSelector) bool {
	if a == nil || b == nil {
		return false
	}
	return MatchLabels(a.MatchLabels, b.MatchLabels) || MatchLabels(b.MatchLabels, a.MatchLabels)
}

func EndpointReadyCount(slices []discoveryv1.EndpointSlice) (total, ready int) {
	for _, s := range slices {
		for _, ep := range s.Endpoints {
			total++
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				ready++
			}
		}
	}
	return
}
