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
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/events"
	"kube-sre-mcp/internal/graph"
	"kube-sre-mcp/internal/health"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/logs"
	"kube-sre-mcp/internal/metrics"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

type Engine struct {
	Cfg     config.Config
	Cluster *kube.ClusterClient
	cache   *apiCache
}

func (e *Engine) Diagnose(ctx context.Context, kind, name, namespace string) model.DiagnosticResponse {
	work := *e
	if work.cache == nil {
		work.cache = newAPICache()
	}
	return work.diagnose(ctx, kind, name, namespace)
}

func (e *Engine) diagnose(ctx context.Context, kind, name, namespace string) model.DiagnosticResponse {
	kind = kube.NormalizeKind(kind)
	cluster := e.Cluster.Info()
	res := kube.NewResolver(e.Cluster, e.Cfg.PrefixMatch)
	resolved := res.Resolve(ctx, kind, name, namespace)
	target := model.ResourceRef{Kind: kind, Name: name, Namespace: namespace}
	switch resolved.Status {
	case kube.ResolveDenied:
		vis := &model.Visibility{Complete: false, Limited: true, MissingPermissions: []model.Permission{{Verb: "get", Resource: kind, Message: resolved.Message}}}
		return permissionResp(cluster, target, vis, resolved.Message)
	case kube.ResolveNotFound:
		return notFoundResp(cluster, target, resolved.Message)
	case kube.ResolveAmbiguous:
		var ev []model.Evidence
		for _, m := range resolved.Matches {
			ev = append(ev, model.Evidence{Source: "resolution", Message: m.String()})
		}
		return model.DiagnosticResponse{
			Status: "error", Cluster: cluster, Target: target, Health: model.HealthUnknown,
			Summary: resolved.Message, Findings: []model.Finding{}, Evidence: ev,
			Recommendations: []model.RecommendedAction{{Priority: 1, Action: "Re-run with an explicit namespace."}},
			Raw:             map[string]any{"ambiguous_matches": resolved.Matches},
		}
	}
	match := resolved.Match
	obj, err := res.Get(ctx, kind, match.Name, match.Namespace)
	if err != nil {
		if kube.IsForbidden(err) {
			vis := &model.Visibility{Limited: true, MissingPermissions: []model.Permission{{Verb: "get", Resource: kind, Namespace: match.Namespace, Message: err.Error()}}}
			return permissionResp(cluster, *match, vis, err.Error())
		}
		if kube.IsNotFound(err) {
			return notFoundResp(cluster, *match, err.Error())
		}
		return errorResp(cluster, *match, err.Error())
	}
	cs := e.Cluster.Clientset
	var out model.DiagnosticResponse
	switch kind {
	case "Pod":
		var p corev1.Pod
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &p)
		out = e.diagnosePod(ctx, &p)
	case "Deployment":
		var d appsv1.Deployment
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &d)
		out = e.diagnoseDeployment(ctx, cs, &d)
	case "StatefulSet":
		var s appsv1.StatefulSet
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &s)
		out = e.diagnoseSTS(ctx, cs, &s)
	case "DaemonSet":
		var d appsv1.DaemonSet
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &d)
		out = e.diagnoseDS(ctx, cs, &d)
	case "Job":
		var j batchv1.Job
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &j)
		out = e.diagnoseJob(ctx, cs, &j)
	case "CronJob":
		var c batchv1.CronJob
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &c)
		out = e.diagnoseCron(ctx, cs, &c)
	case "Service":
		var s corev1.Service
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &s)
		out = e.diagnoseService(ctx, cs, &s)
	case "Ingress":
		var ing networkingv1.Ingress
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &ing)
		out = e.diagnoseIngress(ctx, cs, &ing)
	case "PersistentVolumeClaim":
		var pvc corev1.PersistentVolumeClaim
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &pvc)
		out = e.diagnosePVC(ctx, cs, &pvc)
	case "Node":
		var n corev1.Node
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &n)
		out = e.diagnoseNode(ctx, cs, &n)
	default:
		out = genericDiagnose(cluster, *match, obj)
	}
	if resolved.Resolution != "" {
		out.Resolution = resolved.Resolution
		if resolved.Resolution == "prefix" {
			demoteConfirmed(&out)
		}
	}
	return out
}

func (e *Engine) eventSigs(ctx context.Context, ns, name, kind string) []signal.DiagnosticSignal {
	key := cacheKey("events", kind, ns, name)
	v, err := e.cache.get(ctx, key, func() (any, error) {
		svc := events.Service{CS: e.Cluster.Clientset}
		recs, err := svc.List(ctx, ns, name, kind, kube.IsClusterScopedKind(kind), e.Cfg.MaxEvents)
		ref := model.ResourceRef{Kind: kind, Name: name, Namespace: ns}
		if err != nil {
			return []signal.DiagnosticSignal{signal.New("E-EVT-ERR", signal.SourceEvent, "Events", "list_failed", model.SeverityWarning, model.ConfidenceLow, ref, err.Error())}, err
		}
		return events.ToSignals(ref, recs), nil
	})
	if v == nil {
		return nil
	}
	sigs := v.([]signal.DiagnosticSignal)
	_ = err
	return sigs
}

func (e *Engine) diagnosePod(ctx context.Context, p *corev1.Pod) model.DiagnosticResponse {
	vis := model.NewVisibility()
	in := PodInput{Pod: p, Events: e.eventSigs(ctx, p.Namespace, p.Name, "Pod"), ConfigMaps: map[string]*corev1.ConfigMap{}, SecretsExist: map[string]bool{}, SecretKeys: map[string][]string{}, PVCs: map[string]*corev1.PersistentVolumeClaim{}, ConfigMapUnknown: map[string]bool{}, SecretUnknown: map[string]bool{}, PVCUnknown: map[string]bool{}, Visibility: vis}
	if p.Spec.NodeName != "" {
		key := cacheKey("get", "nodes", "", p.Spec.NodeName)
		v, err := e.cache.get(ctx, key, func() (any, error) {
			return e.Cluster.Clientset.CoreV1().Nodes().Get(ctx, p.Spec.NodeName, metav1.GetOptions{})
		})
		if err == nil && v != nil {
			in.Node = v.(*corev1.Node)
		} else if kube.IsVisibilityFailure(err) {
			kube.Record(vis, "get", "nodes", "", err)
		}
		in.Events = append(in.Events, e.eventSigs(ctx, "", p.Spec.NodeName, "Node")...)
	} else if p.Status.Phase == corev1.PodPending {
		if list, err := e.Cluster.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
			in.CandidatesListed = true
			for i := range list.Items {
				in.CandidateNodes = append(in.CandidateNodes, &list.Items[i])
			}
		} else if kube.IsVisibilityFailure(err) {
			kube.Record(vis, "list", "nodes", "", err)
		}
		if p.Spec.Affinity != nil && p.Spec.Affinity.PodAntiAffinity != nil && p.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			in.UnsupportedConstraints = append(in.UnsupportedConstraints, "required pod anti-affinity is not evaluated (unsupported_scheduler_constraint)")
		}
		if len(p.Spec.TopologySpreadConstraints) > 0 {
			in.UnsupportedConstraints = append(in.UnsupportedConstraints, "topologySpreadConstraints are not evaluated (unsupported_scheduler_constraint)")
		}
	}
	gb := graph.Builder{CS: e.Cluster.Clientset, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.Pod(ctx, p)
	if gerr != nil {
		kube.Record(vis, "graph", "pod", p.Namespace, gerr)
	}
	e.loadPodDeps(ctx, p, &in, vis)
	ls := logs.Service{CS: e.Cluster.Clientset}
	if ctr := logs.SelectFailingContainer(p); ctr != "" {
		res := ls.Get(ctx, p.Name, p.Namespace, ctr, false, int64(min(e.Cfg.MaxLogLines, 80)), nil, true)
		in.LogSignals = append(in.LogSignals, logs.Analyze(graph.PodRef(p), ctr, res.Lines)...)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name == ctr && cs.RestartCount > 0 {
				prev := ls.Get(ctx, p.Name, p.Namespace, ctr, true, 40, nil, true)
				in.LogSignals = append(in.LogSignals, logs.Analyze(graph.PodRef(p), ctr, prev.Lines)...)
			}
		}
	}
	mc := metrics.Client{MC: e.Cluster.Metrics}
	if pm, ok := mc.Pod(ctx, p.Namespace, p.Name); ok {
		in.MetricsAvail = true
		in.HighMemory = metrics.HighMemoryVsLimit(p, pm)
		if in.HighMemory {
			in.LogSignals = append(in.LogSignals, signal.New("E-MET-MEM", signal.SourceMetric, "Memory", "high_memory", model.SeverityWarning, model.ConfidenceLow, graph.PodRef(p), "current memory usage is high versus container limit (not proof of a leak or future OOM)"))
		}
	}
	resp := DiagnosePod(e.Cluster.Info(), in)
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated
		resp.TruncationReason = g.TruncationReason
	}
	resp.MetricsAvailable = boolPtr(in.MetricsAvail)
	return resp
}

func (e *Engine) loadPodDeps(ctx context.Context, p *corev1.Pod, in *PodInput, vis *model.Visibility) {
	cs := e.Cluster.Clientset
	getCM := func(name string) {
		if name == "" || in.ConfigMaps[name] != nil || in.ConfigMapUnknown[name] {
			return
		}
		key := cacheKey("get", "configmaps", p.Namespace, name)
		v, err := e.cache.get(ctx, key, func() (any, error) {
			return cs.CoreV1().ConfigMaps(p.Namespace).Get(ctx, name, metav1.GetOptions{})
		})
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			in.ConfigMapUnknown[name] = true
			kube.Record(vis, "get", "configmaps", p.Namespace, err)
			return
		}
		in.ConfigMaps[name] = v.(*corev1.ConfigMap)
	}
	getSec := func(name string) {
		if name == "" {
			return
		}
		if _, known := in.SecretsExist[name]; known || in.SecretUnknown[name] {
			return
		}
		key := cacheKey("get", "secrets", p.Namespace, name)
		v, err := e.cache.get(ctx, key, func() (any, error) {
			return cs.CoreV1().Secrets(p.Namespace).Get(ctx, name, metav1.GetOptions{})
		})
		if apierrors.IsNotFound(err) {
			in.SecretsExist[name] = false
			return
		}
		if err != nil {
			in.SecretUnknown[name] = true
			kube.Record(vis, "get", "secrets", p.Namespace, err)
			return
		}
		sec := v.(*corev1.Secret)
		in.SecretsExist[name] = true
		keys := make([]string, 0, len(sec.Data))
		for k := range sec.Data {
			keys = append(keys, k)
		}
		in.SecretKeys[name] = keys
	}
	walk := func(c corev1.Container) {
		for _, env := range c.Env {
			if env.ValueFrom == nil {
				continue
			}
			if env.ValueFrom.ConfigMapKeyRef != nil {
				getCM(env.ValueFrom.ConfigMapKeyRef.Name)
			}
			if env.ValueFrom.SecretKeyRef != nil {
				getSec(env.ValueFrom.SecretKeyRef.Name)
			}
		}
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				getCM(ef.ConfigMapRef.Name)
			}
			if ef.SecretRef != nil {
				getSec(ef.SecretRef.Name)
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
			getCM(v.ConfigMap.Name)
		}
		if v.Secret != nil {
			getSec(v.Secret.SecretName)
		}
		if v.PersistentVolumeClaim != nil {
			claim := v.PersistentVolumeClaim.ClaimName
			key := cacheKey("get", "persistentvolumeclaims", p.Namespace, claim)
			vobj, err := e.cache.get(ctx, key, func() (any, error) {
				return cs.CoreV1().PersistentVolumeClaims(p.Namespace).Get(ctx, claim, metav1.GetOptions{})
			})
			if err == nil {
				pvc := vobj.(*corev1.PersistentVolumeClaim)
				in.PVCs[pvc.Name] = pvc
			} else if !apierrors.IsNotFound(err) {
				in.PVCUnknown[claim] = true
				kube.Record(vis, "get", "persistentvolumeclaims", p.Namespace, err)
			}
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.Secret != nil {
					getSec(src.Secret.Name)
				}
				if src.ConfigMap != nil {
					getCM(src.ConfigMap.Name)
				}
			}
		}
	}
	if p.Spec.ServiceAccountName != "" {
		if sa, err := cs.CoreV1().ServiceAccounts(p.Namespace).Get(ctx, p.Spec.ServiceAccountName, metav1.GetOptions{}); err == nil {
			in.ServiceAccount = sa
		} else if !apierrors.IsNotFound(err) {
			in.SAUnknown = true
			kube.Record(vis, "get", "serviceaccounts", p.Namespace, err)
		}
	}
	for _, s := range p.Spec.ImagePullSecrets {
		getSec(s.Name)
	}
}

func (e *Engine) diagnoseDeployment(ctx context.Context, cs kubernetes.Interface, d *appsv1.Deployment) model.DiagnosticResponse {
	vis := model.NewVisibility()
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.Deployment(ctx, d)
	if gerr != nil {
		kube.Record(vis, "graph", "deployments", d.Namespace, gerr)
	}
	rss, err := cs.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		kube.Record(vis, "list", "replicasets", d.Namespace, err)
	}
	var current, old []appsv1.ReplicaSet
	if rss != nil {
		current, old = graph.CurrentReplicaSets(d, rss.Items)
	}
	sel := ""
	if d.Spec.Selector != nil {
		sel = metav1.FormatLabelSelector(d.Spec.Selector)
	}
	list, err := cs.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		kube.Record(vis, "list", "pods", d.Namespace, err)
	}
	currentUIDs := map[types.UID]struct{}{}
	for i := range current {
		currentUIDs[current[i].UID] = struct{}{}
	}
	var pods []corev1.Pod
	if list != nil {
		for i := range list.Items {
			p := list.Items[i]
			owned := false
			for _, o := range p.OwnerReferences {
				if _, ok := currentUIDs[o.UID]; ok {
					owned = true
					break
				}
			}
			if owned {
				pods = append(pods, p)
			}
		}
	}
	podSigs := map[string][]signal.DiagnosticSignal{}
	deep := 0
	unhealthy := countUnhealthy(pods)
	podSigs, deep = e.deepPodSignals(ctx, pods, e.Cfg.MaxDeepPods, true)
	resp := DiagnoseDeployment(e.Cluster.Info(), d, pods, podSigs, e.eventSigs(ctx, d.Namespace, d.Name, "Deployment"))
	if len(old) > 0 {
		resp.Evidence = append(resp.Evidence, model.Evidence{Source: "STATUS", Message: fmt.Sprintf("%d older ReplicaSet(s) excluded from current workload RCA", len(old))})
	}
	if vis.Limited {
		resp.Visibility = vis
		if resp.Status == "success" {
			resp.Status = "partial"
		}
		if resp.Health == model.HealthHealthy {
			resp.Health = health.Evaluate(nil, vis)
		}
	}
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated || (deep >= e.Cfg.MaxDeepPods && unhealthy > deep)
		resp.TruncationReason = g.TruncationReason
		for _, n := range g.OfKind("ReplicaSet") {
			resp.Evidence = append(resp.Evidence, evidences(e.eventSigs(ctx, n.Namespace, n.Name, "ReplicaSet"))...)
		}
	}
	return resp
}

func (e *Engine) diagnoseSTS(ctx context.Context, cs kubernetes.Interface, s *appsv1.StatefulSet) model.DiagnosticResponse {
	vis := model.NewVisibility()
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.StatefulSet(ctx, s)
	if gerr != nil {
		kube.Record(vis, "graph", "statefulsets", s.Namespace, gerr)
	}
	list, err := cs.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(s.Spec.Selector)})
	if err != nil {
		kube.Record(vis, "list", "pods", s.Namespace, err)
	}
	var pods []corev1.Pod
	if list != nil {
		for i := range list.Items {
			if graph.OwnedByUID(&list.Items[i], s.UID) {
				pods = append(pods, list.Items[i])
			}
		}
	}
	var pvcs []corev1.PersistentVolumeClaim
	if pl, err := cs.CoreV1().PersistentVolumeClaims(s.Namespace).List(ctx, metav1.ListOptions{}); err != nil {
		kube.Record(vis, "list", "persistentvolumeclaims", s.Namespace, err)
	} else {
		for i := range pl.Items {
			if graph.STSOwnsPVC(s, &pl.Items[i]) {
				pvcs = append(pvcs, pl.Items[i])
			}
		}
	}
	podSigs, _ := e.deepPodSignals(ctx, pods, e.Cfg.MaxDeepPods, true)
	resp := DiagnoseStatefulSet(e.Cluster.Info(), s, pods, pvcs, podSigs, e.eventSigs(ctx, s.Namespace, s.Name, "StatefulSet"))
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated
		resp.TruncationReason = g.TruncationReason
	}
	if vis.Limited {
		resp.Visibility = vis
		resp.Status = "partial"
	}
	return resp
}

func (e *Engine) diagnoseDS(ctx context.Context, cs kubernetes.Interface, d *appsv1.DaemonSet) model.DiagnosticResponse {
	vis := model.NewVisibility()
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.DaemonSet(ctx, d)
	if gerr != nil {
		kube.Record(vis, "graph", "daemonsets", d.Namespace, gerr)
	}
	list, err := cs.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(d.Spec.Selector)})
	if err != nil {
		kube.Record(vis, "list", "pods", d.Namespace, err)
	}
	var pods []corev1.Pod
	if list != nil {
		for i := range list.Items {
			if graph.OwnedByUID(&list.Items[i], d.UID) {
				pods = append(pods, list.Items[i])
			}
		}
	}
	podSigs, _ := e.deepPodSignals(ctx, pods, e.Cfg.MaxDeepPods, true)
	resp := DiagnoseDaemonSet(e.Cluster.Info(), d, pods, podSigs, e.eventSigs(ctx, d.Namespace, d.Name, "DaemonSet"))
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated
		resp.TruncationReason = g.TruncationReason
	}
	if vis.Limited {
		resp.Visibility = vis
		resp.Status = "partial"
	}
	return resp
}

func (e *Engine) diagnoseJob(ctx context.Context, cs kubernetes.Interface, j *batchv1.Job) model.DiagnosticResponse {
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, _ := gb.Job(ctx, j)
	list, _ := cs.CoreV1().Pods(j.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + j.Name})
	var pods []corev1.Pod
	if list != nil {
		pods = list.Items
	}
	podSigs, _ := e.deepPodSignals(ctx, pods, e.Cfg.MaxDeepPods, false)
	resp := DiagnoseJob(e.Cluster.Info(), j, pods, podSigs, e.eventSigs(ctx, j.Namespace, j.Name, "Job"))
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
	}
	return resp
}

func (e *Engine) diagnoseCron(ctx context.Context, cs kubernetes.Interface, c *batchv1.CronJob) model.DiagnosticResponse {
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, _ := gb.CronJob(ctx, c)
	jobs, _ := cs.BatchV1().Jobs(c.Namespace).List(ctx, metav1.ListOptions{})
	var mine []batchv1.Job
	jsigs := map[string][]signal.DiagnosticSignal{}
	if jobs != nil {
		for i := range jobs.Items {
			j := jobs.Items[i]
			if !graph.OwnedByUID(&j, c.UID) {
				continue
			}
			mine = append(mine, j)
		}
	}
	capN := e.Cfg.MaxDeepPods
	if capN <= 0 {
		capN = 8
	}
	n := len(mine)
	if n > capN {
		n = capN
	}
	tmp := make([][]signal.DiagnosticSignal, n)
	e.runBounded(ctx, n, func(i int) {
		tmp[i] = sigsFromResp(e.diagnoseJob(ctx, cs, &mine[i]))
	})
	for i := 0; i < n; i++ {
		jsigs[mine[i].Name] = tmp[i]
	}
	resp := DiagnoseCronJob(e.Cluster.Info(), c, mine, jsigs, e.eventSigs(ctx, c.Namespace, c.Name, "CronJob"))
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
	}
	return resp
}

func (e *Engine) diagnoseService(ctx context.Context, cs kubernetes.Interface, s *corev1.Service) model.DiagnosticResponse {
	vis := model.NewVisibility()
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.Service(ctx, s)
	if gerr != nil {
		kube.Record(vis, "graph", "services", s.Namespace, gerr)
	}
	var pods []corev1.Pod
	if sel := graph.SelectorString(s.Spec.Selector); sel != "" {
		if list, err := cs.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel}); err == nil {
			pods = list.Items
		} else {
			kube.Record(vis, "list", "pods", s.Namespace, err)
		}
	}
	slicesKnown := true
	var slices []discoveryv1.EndpointSlice
	if list, err := cs.DiscoveryV1().EndpointSlices(s.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + s.Name}); err == nil {
		slices = list.Items
	} else {
		slicesKnown = false
		kube.Record(vis, "list", "endpointslices", s.Namespace, err)
	}
	var eps *corev1.Endpoints
	if ep, err := cs.CoreV1().Endpoints(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{}); err == nil {
		eps = ep
	} else if !apierrors.IsNotFound(err) {
		kube.Record(vis, "get", "endpoints", s.Namespace, err)
	}
	podSigs, _ := e.deepPodSignals(ctx, pods, e.Cfg.MaxDeepPods, true)
	resp := DiagnoseService(e.Cluster.Info(), s, pods, slices, eps, podSigs, e.eventSigs(ctx, s.Namespace, s.Name, "Service"), slicesKnown)
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated
		resp.TruncationReason = g.TruncationReason
	}
	if vis.Limited {
		resp.Visibility = vis
		resp.Status = "partial"
		if resp.Health == model.HealthHealthy {
			resp.Health = model.HealthUnknown
		}
	}
	return resp
}

func (e *Engine) diagnoseIngress(ctx context.Context, cs kubernetes.Interface, ing *networkingv1.Ingress) model.DiagnosticResponse {
	vis := model.NewVisibility()
	gb := graph.Builder{CS: cs, Limits: graph.Limits{MaxDepth: e.Cfg.MaxGraphDepth, MaxNodes: e.Cfg.MaxGraphNodes}}
	g, gerr := gb.Ingress(ctx, ing)
	if gerr != nil {
		kube.Record(vis, "graph", "ingresses", ing.Namespace, gerr)
	}
	svcs := map[string]*corev1.Service{}
	sec := map[string]bool{}
	add := func(name string) {
		if name == "" {
			return
		}
		s, err := cs.CoreV1().Services(ing.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			svcs[name] = s
			return
		}
		if !apierrors.IsNotFound(err) {
			kube.Record(vis, "get", "services", ing.Namespace, err)
		}
	}
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		add(ing.Spec.DefaultBackend.Service.Name)
	}
	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			continue
		}
		for _, p := range r.HTTP.Paths {
			if p.Backend.Service != nil {
				add(p.Backend.Service.Name)
			}
		}
	}
	esMap := map[string][]discoveryv1.EndpointSlice{}
	for name := range svcs {
		if list, err := cs.DiscoveryV1().EndpointSlices(ing.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + name}); err == nil {
			esMap[name] = list.Items
		}
	}
	for _, t := range ing.Spec.TLS {
		if t.SecretName == "" {
			continue
		}
		_, err := cs.CoreV1().Secrets(ing.Namespace).Get(ctx, t.SecretName, metav1.GetOptions{})
		sec[t.SecretName] = err == nil
	}
	var classOK *bool
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != "" {
		_, err := cs.NetworkingV1().IngressClasses().Get(ctx, *ing.Spec.IngressClassName, metav1.GetOptions{})
		v := err == nil
		classOK = &v
	}
	resp := DiagnoseIngress(e.Cluster.Info(), ing, svcs, esMap, sec, classOK, e.eventSigs(ctx, ing.Namespace, ing.Name, "Ingress"))
	if g != nil {
		resp.Dependencies = depsFromGraph(g)
		resp.Truncated = g.Truncated
		resp.TruncationReason = g.TruncationReason
	}
	if vis.Limited {
		resp.Visibility = vis
		resp.Status = "partial"
	}
	return resp
}

func (e *Engine) diagnosePVC(ctx context.Context, cs kubernetes.Interface, pvc *corev1.PersistentVolumeClaim) model.DiagnosticResponse {
	var pv *corev1.PersistentVolume
	if pvc.Spec.VolumeName != "" {
		pv, _ = cs.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	}
	var sc *storagev1.StorageClass
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		sc, _ = cs.StorageV1().StorageClasses().Get(ctx, *pvc.Spec.StorageClassName, metav1.GetOptions{})
	}
	return DiagnosePVC(e.Cluster.Info(), pvc, pv, sc, e.eventSigs(ctx, pvc.Namespace, pvc.Name, "PersistentVolumeClaim"))
}

func (e *Engine) diagnoseNode(ctx context.Context, cs kubernetes.Interface, n *corev1.Node) model.DiagnosticResponse {
	hosted := 0
	if list, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + n.Name}); err == nil {
		hosted = len(list.Items)
	}
	mc := metrics.Client{MC: e.Cluster.Metrics}
	_, ok := mc.Node(ctx, n.Name)
	return DiagnoseNode(e.Cluster.Info(), n, hosted, e.eventSigs(ctx, "", n.Name, "Node"), ok)
}

func genericDiagnose(cluster model.ClusterInfo, target model.ResourceRef, obj *unstructured.Unstructured) model.DiagnosticResponse {
	var sigs []signal.DiagnosticSignal
	conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	id := 1
	if found {
		for _, c := range conds {
			m, _ := c.(map[string]any)
			typ, _ := m["type"].(string)
			st, _ := m["status"].(string)
			msg, _ := m["message"].(string)
			reason, _ := m["reason"].(string)
			if st == "False" || typ == "Degraded" {
				sigs = append(sigs, signal.New(fmt.Sprintf("E-GEN-%d", id), signal.SourceCondition, "Generic", reason, model.SeverityWarning, model.ConfidenceLow, target, typ+"="+st+": "+msg))
				id++
			}
		}
	}
	resp := assemble(cluster, target, sigs, nil, "generic")
	if resp.Health == model.HealthHealthy && len(sigs) == 0 {
		resp.Health = model.HealthUnknown
		resp.Summary = fmt.Sprintf("%s %q generic inspection. No domain-specific analyzer; diagnostic_depth=generic.", target.Kind, target.Name)
	}
	resp.DiagnosticDepth = "generic"
	return resp
}

func GenericDiagnoseForTest(cluster model.ClusterInfo, target model.ResourceRef, obj *unstructured.Unstructured) model.DiagnosticResponse {
	return genericDiagnose(cluster, target, obj)
}

func depsFromGraph(g *graph.Graph) []model.Dependency {
	var out []model.Dependency
	for _, e := range g.Edges {
		out = append(out, model.Dependency{Type: string(e.Type), Resource: e.To})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Resource.String() < out[j].Resource.String()
	})
	return out
}

func sigsFromResp(r model.DiagnosticResponse) []signal.DiagnosticSignal {
	var out []signal.DiagnosticSignal
	for _, ev := range r.Evidence {
		if ev.ID == "" {
			continue
		}
		src := signal.Source(ev.Source)
		if src == "" {
			src = signal.SourceStatus
		}
		ref := ev.Resource
		if ref.Name == "" {
			ref = r.Target
		}
		out = append(out, signal.New(ev.ID, src, "Pod", "", model.SeverityWarning, model.ConfidenceHigh, ref, ev.Message))
	}
	return out
}

func demoteConfirmed(resp *model.DiagnosticResponse) {
	if resp == nil {
		return
	}
	if resp.RootCause != nil && resp.RootCause.Confidence == model.ConfidenceConfirmed {
		resp.RootCause.Confidence = model.ConfidenceHigh
	}
	for i := range resp.RootCauses {
		if resp.RootCauses[i].Confidence == model.ConfidenceConfirmed {
			resp.RootCauses[i].Confidence = model.ConfidenceHigh
		}
	}
}

func (e *Engine) deepPodSignals(ctx context.Context, pods []corev1.Pod, capN int, unhealthyOnly bool) (map[string][]signal.DiagnosticSignal, int) {
	if capN <= 0 {
		capN = 8
	}
	var idx []int
	for i := range pods {
		if unhealthyOnly && !UnhealthyPod(&pods[i]) {
			continue
		}
		idx = append(idx, i)
		if len(idx) >= capN {
			break
		}
	}
	tmp := make([][]signal.DiagnosticSignal, len(idx))
	names := make([]string, len(idx))
	e.runBounded(ctx, len(idx), func(i int) {
		p := &pods[idx[i]]
		names[i] = p.Name
		tmp[i] = sigsFromResp(e.diagnosePod(ctx, p))
	})
	m := map[string][]signal.DiagnosticSignal{}
	for i := range idx {
		if names[i] == "" {
			continue
		}
		m[names[i]] = tmp[i]
	}
	return m, len(idx)
}

func evidences(ss []signal.DiagnosticSignal) []model.Evidence {
	var out []model.Evidence
	for _, s := range ss {
		out = append(out, s.ToEvidence())
	}
	return out
}

func countUnhealthy(pods []corev1.Pod) int {
	n := 0
	for i := range pods {
		if UnhealthyPod(&pods[i]) {
			n++
		}
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
