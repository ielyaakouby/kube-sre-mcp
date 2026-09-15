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

package action

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/graph"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/recommendation"
)

type Engine struct {
	Cfg        config.Config
	Cluster    *kube.ClusterClient
	Store      *Store
	ForContext func(string) (*kube.ClusterClient, error)
}

type Result struct {
	Status         string            `json:"status"`
	Message        string            `json:"message,omitempty"`
	ConfirmationID string            `json:"confirmation_id,omitempty"`
	Action         string            `json:"action,omitempty"`
	Risk           Risk              `json:"risk,omitempty"`
	Authorized     bool              `json:"authorized"`
	AuthReason     string            `json:"authorization,omitempty"`
	BlastRadius    any               `json:"blast_radius,omitempty"`
	Preconditions  []string          `json:"preconditions,omitempty"`
	Plan           *model.ActionPlan `json:"plan,omitempty"`
}

func (e *Engine) disabled() Result {
	return Result{Status: "disabled", Message: "write actions are disabled (KUBE_SRE_MCP_ACTIONS_ENABLED=false)"}
}

func (e *Engine) clusterFor(ctxName string) (*kube.ClusterClient, error) {
	if e.Cluster != nil && (ctxName == "" || ctxName == e.Cluster.ContextName) {
		return e.Cluster, nil
	}
	if e.ForContext != nil {
		return e.ForContext(ctxName)
	}
	if e.Cluster == nil {
		return nil, fmt.Errorf("no kubernetes cluster client")
	}
	if ctxName != "" && ctxName != e.Cluster.ContextName {
		return nil, fmt.Errorf("context %q is not available on this process", ctxName)
	}
	return e.Cluster, nil
}

func (e *Engine) confirm(want Pending, confirmID string) (Result, Pending) {
	if confirmID == "" {
		id, err := e.Store.Put(want)
		if err != nil {
			return Result{Status: "error", Message: errMsg(err)}, Pending{}
		}
		return Result{Status: "confirmation_required", ConfirmationID: id, Action: want.Action, Authorized: true}, Pending{}
	}
	stored, err := e.Store.Take(confirmID, want)
	if err != nil {
		st := "error"
		if err == ErrConfirmationMismatch {
			st = "confirmation_mismatch"
		}
		return Result{Status: st, Message: errMsg(err)}, Pending{}
	}
	return Result{Status: "ok"}, stored
}

func (e *Engine) RestartDeployment(ctx context.Context, name, ns, confirmID, rcaCategory, kubeCtx string, force bool) Result {
	if !e.Cfg.ActionsEnabled {
		return e.disabled()
	}
	cl, err := e.clusterFor(kubeCtx)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	cs := cl.Clientset
	ok, reason, err := SelfSubjectAllowed(ctx, cs, "patch", "deployments", "apps", ns, name)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	if !ok {
		return Result{Status: "forbidden", Authorized: false, AuthReason: redactText(reason), Message: redactText(reason)}
	}
	d, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	cat := rcaCategory
	if cat == "" {
		cat = inferRestartCategory(ctx, cs, d)
	}
	if cat != "" && !recommendation.RestartUseful(cat) && !force {
		return Result{Status: "restart_not_recommended", Authorized: true, Message: "restart is unlikely to fix " + cat, Action: "restart_deployment"}
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	blast := map[string]any{"desired": desired, "available": d.Status.AvailableReplicas, "strategy": d.Spec.Strategy.Type, "context": cl.ContextName}
	risk := RiskHigh
	pre := []string{"mutating action requires confirmation"}
	if !recommendation.RestartUseful(cat) && cat != "" {
		pre = append(pre, "Restart is unlikely to fix "+cat)
		risk = RiskCritical
	}
	blocked, pdbErr, pdbNames := matchingPDBBlock(ctx, cs, ns, d.Spec.Selector)
	if pdbErr != nil {
		return Result{Status: "preflight_failed", Message: "PDB visibility limited: " + errMsg(pdbErr), Authorized: true}
	}
	if blocked {
		return Result{Status: "blocked_by_pdb", Authorized: true, Message: "PodDisruptionBudget disruptionsAllowed=0", Preconditions: pdbNames}
	}
	if len(pdbNames) > 0 {
		pre = append(pre, "PDB present: "+fmt.Sprint(pdbNames))
	}
	want := Pending{Action: "restart_deployment", APIGroup: "apps", Kind: "Deployment", Name: name, Namespace: ns, UID: string(d.UID), Context: cl.ContextName}
	cr, stored := e.confirm(want, confirmID)
	if cr.Status != "ok" {
		cr.Risk = risk
		cr.Authorized = true
		cr.BlastRadius = blast
		cr.Preconditions = pre
		return cr
	}
	live, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	if string(live.UID) != stored.UID {
		return Result{Status: "confirmation_mismatch", Message: "object UID changed since confirmation; refusing to mutate a recreated object", Authorized: true}
	}
	blocked, pdbErr, pdbNames = matchingPDBBlock(ctx, cs, ns, live.Spec.Selector)
	if pdbErr != nil {
		return Result{Status: "preflight_failed", Message: "PDB visibility limited at execution: " + errMsg(pdbErr), Authorized: true}
	}
	if blocked {
		return Result{Status: "blocked_by_pdb", Authorized: true, Message: "PodDisruptionBudget disruptionsAllowed=0", Preconditions: pdbNames}
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, time.Now().UTC().Format(time.RFC3339))
	_, err = cs.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	return Result{Status: "success", Authorized: true, Message: fmt.Sprintf("Deployment %q rollout restart triggered.", name), BlastRadius: blast}
}

func (e *Engine) Scale(ctx context.Context, kind, name, ns string, replicas int32, confirmID, kubeCtx string) Result {
	if !e.Cfg.ActionsEnabled {
		return e.disabled()
	}
	kind = kube.NormalizeKind(kind)
	if replicas < 0 {
		return Result{Status: "error", Message: "replicas cannot be negative"}
	}
	if replicas > e.Cfg.MaxReplicas {
		return Result{Status: "error", Message: fmt.Sprintf("replicas %d exceeds configured maximum %d", replicas, e.Cfg.MaxReplicas)}
	}
	res, group := "deployments", "apps"
	switch kind {
	case "StatefulSet":
		res = "statefulsets"
	case "ReplicaSet":
		res = "replicasets"
	case "Deployment":
	default:
		return Result{Status: "error", Message: "scaling not supported for kind " + kind}
	}
	cl, err := e.clusterFor(kubeCtx)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	cs := cl.Clientset
	ok, reason, err := SelfSubjectAllowed(ctx, cs, "patch", res, group, ns, name)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	if !ok {
		return Result{Status: "forbidden", Authorized: false, AuthReason: redactText(reason), Message: redactText(reason)}
	}
	current, uid := int32(0), ""
	switch kind {
	case "Deployment":
		d, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return Result{Status: "error", Message: errMsg(err)}
		}
		if d.Spec.Replicas != nil {
			current = *d.Spec.Replicas
		}
		uid = string(d.UID)
	case "StatefulSet":
		s, err := cs.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return Result{Status: "error", Message: errMsg(err)}
		}
		if s.Spec.Replicas != nil {
			current = *s.Spec.Replicas
		}
		uid = string(s.UID)
	case "ReplicaSet":
		rs, err := cs.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return Result{Status: "error", Message: errMsg(err)}
		}
		if rs.Spec.Replicas != nil {
			current = *rs.Spec.Replicas
		}
		uid = string(rs.UID)
	}
	hpaOwns, hpaErr := hasHPA(ctx, cs, ns, kind, name)
	if hpaErr != nil {
		return Result{Status: "preflight_failed", Message: "HPA visibility limited: " + errMsg(hpaErr), Authorized: true}
	}
	risk := RiskHigh
	pre := []string{fmt.Sprintf("current=%d requested=%d", current, replicas), "mutating action requires confirmation"}
	if hpaOwns {
		pre = append(pre, "HPA owns replica count; scaling may be reverted")
		risk = RiskCritical
	}
	rep := replicas
	want := Pending{Action: "scale", APIGroup: "apps", Kind: kind, Name: name, Namespace: ns, UID: uid, Context: cl.ContextName, Replicas: &rep, HPAPresent: hpaOwns}
	cr, stored := e.confirm(want, confirmID)
	if cr.Status != "ok" {
		cr.Risk = risk
		cr.Authorized = true
		cr.BlastRadius = map[string]any{"from": current, "to": replicas, "context": cl.ContextName}
		cr.Preconditions = pre
		return cr
	}
	liveUID, err := currentUID(ctx, cs, kind, ns, name)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	if liveUID != stored.UID {
		return Result{Status: "confirmation_mismatch", Message: "object UID changed since confirmation; refusing to mutate a recreated object", Authorized: true}
	}
	hpaNow, hpaErr := hasHPA(ctx, cs, ns, kind, name)
	if hpaErr != nil {
		return Result{Status: "preflight_failed", Message: "HPA visibility limited at execution: " + errMsg(hpaErr), Authorized: true}
	}
	if hpaNow && !stored.HPAPresent {
		return Result{Status: "preflight_failed", Message: "HPA created after confirmation; refusing to scale", Authorized: true}
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	var perr error
	switch kind {
	case "Deployment":
		_, perr = cs.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	case "StatefulSet":
		_, perr = cs.AppsV1().StatefulSets(ns).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	case "ReplicaSet":
		_, perr = cs.AppsV1().ReplicaSets(ns).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	}
	if perr != nil {
		return Result{Status: "error", Message: errMsg(perr), Authorized: true}
	}
	return Result{Status: "success", Authorized: true, Message: fmt.Sprintf("%s %q scaled to %d replicas.", kind, name, replicas)}
}

func (e *Engine) DeletePod(ctx context.Context, name, ns string, grace int64, confirmID, kubeCtx string) Result {
	if !e.Cfg.ActionsEnabled {
		return e.disabled()
	}
	cl, err := e.clusterFor(kubeCtx)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	cs := cl.Clientset
	ok, reason, err := SelfSubjectAllowed(ctx, cs, "delete", "pods", "", ns, name)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	if !ok {
		return Result{Status: "forbidden", Authorized: false, AuthReason: redactText(reason), Message: redactText(reason)}
	}
	p, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	if isControlPlanePod(p) {
		return Result{Status: "error", Message: "refusing to delete control-plane/system pod"}
	}
	if isMirrorPod(p) {
		return Result{Status: "error", Message: "refusing to delete static/mirror pod"}
	}
	blocked, pdbErr, pdbNames := matchingPDBBlockForPod(ctx, cs, p)
	if pdbErr != nil {
		return Result{Status: "preflight_failed", Message: "PDB visibility limited: " + errMsg(pdbErr), Authorized: true}
	}
	if blocked {
		return Result{Status: "blocked_by_pdb", Authorized: true, Message: "PodDisruptionBudget disruptionsAllowed=0", Preconditions: pdbNames}
	}
	risk := RiskHigh
	pre := []string{"mutating action requires confirmation"}
	if len(p.OwnerReferences) == 0 {
		pre = append(pre, "pod has no ownerReferences (unmanaged)")
		risk = RiskCritical
	} else if ownerKind(p) == "DaemonSet" || ownerKind(p) == "Job" {
		pre = append(pre, "pod is owned by "+ownerKind(p))
		risk = RiskCritical
	}
	if p.Namespace == "kube-system" {
		pre = append(pre, "kube-system namespace")
		risk = RiskCritical
	}
	g := grace
	want := Pending{Action: "delete_pod", APIGroup: "", Kind: "Pod", Name: name, Namespace: ns, UID: string(p.UID), Context: cl.ContextName, Grace: &g}
	cr, stored := e.confirm(want, confirmID)
	if cr.Status != "ok" {
		cr.Risk = risk
		cr.Authorized = true
		cr.BlastRadius = map[string]any{"unmanaged": len(p.OwnerReferences) == 0, "context": cl.ContextName}
		cr.Preconditions = pre
		return cr
	}
	live, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	if string(live.UID) != stored.UID {
		return Result{Status: "confirmation_mismatch", Message: "object UID changed since confirmation; refusing to mutate a recreated object", Authorized: true}
	}
	blocked, pdbErr, pdbNames = matchingPDBBlockForPod(ctx, cs, live)
	if pdbErr != nil {
		return Result{Status: "preflight_failed", Message: "PDB visibility limited at execution: " + errMsg(pdbErr), Authorized: true}
	}
	if blocked {
		return Result{Status: "blocked_by_pdb", Authorized: true, Message: "PodDisruptionBudget disruptionsAllowed=0", Preconditions: pdbNames}
	}
	gp := grace
	err = cs.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &gp})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	return Result{Status: "success", Authorized: true, Message: fmt.Sprintf("Pod %q deleted.", name)}
}

func (e *Engine) Cordon(ctx context.Context, name, confirmID, kubeCtx string, cordon bool) Result {
	if !e.Cfg.ActionsEnabled {
		return e.disabled()
	}
	cl, err := e.clusterFor(kubeCtx)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	cs := cl.Clientset
	ok, reason, err := SelfSubjectAllowed(ctx, cs, "patch", "nodes", "", "", name)
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	if !ok {
		return Result{Status: "forbidden", Authorized: false, AuthReason: redactText(reason), Message: redactText(reason)}
	}
	act := "cordon"
	if !cordon {
		act = "uncordon"
	}
	target, err := cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err)}
	}
	pre := []string{"mutating action requires confirmation"}
	risk := RiskMedium
	if cordon {
		nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return Result{Status: "preflight_failed", Message: errMsg(err)}
		}
		sched := 0
		for _, n := range nodes.Items {
			if !n.Spec.Unschedulable && nodeReady(&n) {
				sched++
			}
		}
		if !target.Spec.Unschedulable && nodeReady(target) && sched <= 1 {
			return Result{Status: "error", Message: "refusing to cordon the last schedulable Ready node", Risk: RiskCritical, BlastRadius: map[string]any{"schedulable_ready_nodes": sched}}
		}
		if target.Labels["node-role.kubernetes.io/control-plane"] != "" || target.Labels["node-role.kubernetes.io/master"] != "" {
			pre = append(pre, "control-plane node")
			risk = RiskCritical
		} else {
			risk = RiskHigh
		}
	}
	cval := cordon
	want := Pending{Action: act, APIGroup: "", Kind: "Node", Name: name, Namespace: "", UID: string(target.UID), Context: cl.ContextName, Cordon: &cval}
	cr, stored := e.confirm(want, confirmID)
	if cr.Status != "ok" {
		cr.Risk = risk
		cr.Authorized = true
		cr.Preconditions = pre
		cr.BlastRadius = map[string]any{"context": cl.ContextName}
		return cr
	}
	live, err := cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	if string(live.UID) != stored.UID {
		return Result{Status: "confirmation_mismatch", Message: "object UID changed since confirmation; refusing to mutate a recreated object", Authorized: true}
	}
	if cordon {
		nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return Result{Status: "preflight_failed", Message: errMsg(err), Authorized: true}
		}
		sched := 0
		for _, n := range nodes.Items {
			if !n.Spec.Unschedulable && nodeReady(&n) {
				sched++
			}
		}
		if !live.Spec.Unschedulable && nodeReady(live) && sched <= 1 {
			return Result{Status: "error", Message: "refusing to cordon the last schedulable Ready node", Risk: RiskCritical, BlastRadius: map[string]any{"schedulable_ready_nodes": sched}}
		}
	}
	patch := fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, cordon)
	_, err = cs.CoreV1().Nodes().Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return Result{Status: "error", Message: errMsg(err), Authorized: true}
	}
	msg := fmt.Sprintf("Node %q uncordoned (scheduling enabled).", name)
	if cordon {
		msg = fmt.Sprintf("Node %q cordoned (unschedulable).", name)
	}
	return Result{Status: "success", Authorized: true, Message: msg}
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func hasHPA(ctx context.Context, cs kubernetes.Interface, ns, kind, name string) (bool, error) {
	match := func(apiVersion, k, n string) bool {
		if k != kind || n != name {
			return false
		}
		if apiVersion == "" {
			return true
		}
		return apiVersion == "apps/v1" || apiVersion == "apps/v1beta1" || apiVersion == "apps/v1beta2" || strings.HasPrefix(apiVersion, "apps/")
	}
	v2, err := cs.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, h := range v2.Items {
		if match(h.Spec.ScaleTargetRef.APIVersion, h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name) {
			return true, nil
		}
	}
	v1, err := cs.AutoscalingV1().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, h := range v1.Items {
		if match(h.Spec.ScaleTargetRef.APIVersion, h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name) {
			return true, nil
		}
	}
	return false, nil
}

func currentUID(ctx context.Context, cs kubernetes.Interface, kind, ns, name string) (string, error) {
	switch kind {
	case "Deployment":
		d, err := cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return string(d.UID), nil
	case "StatefulSet":
		s, err := cs.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return string(s.UID), nil
	case "ReplicaSet":
		rs, err := cs.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return string(rs.UID), nil
	default:
		return "", fmt.Errorf("unsupported kind")
	}
}

func matchingPDBBlock(ctx context.Context, cs kubernetes.Interface, ns string, sel *metav1.LabelSelector) (bool, error, []string) {
	if sel == nil {
		return false, nil, nil
	}
	list, err := cs.PolicyV1().PodDisruptionBudgets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err, nil
	}
	var names []string
	blocked := false
	for i := range list.Items {
		p := &list.Items[i]
		if !pdbMatchesWorkload(p, sel) {
			continue
		}
		names = append(names, p.Name)
		if p.Status.DisruptionsAllowed == 0 {
			blocked = true
		}
	}
	return blocked, nil, names
}

func matchingPDBBlockForPod(ctx context.Context, cs kubernetes.Interface, pod *corev1.Pod) (bool, error, []string) {
	list, err := cs.PolicyV1().PodDisruptionBudgets(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err, nil
	}
	var names []string
	blocked := false
	for i := range list.Items {
		p := &list.Items[i]
		if p.Spec.Selector == nil {
			continue
		}
		sel, err := metav1.LabelSelectorAsSelector(p.Spec.Selector)
		if err != nil {
			names = append(names, p.Name)
			if p.Status.DisruptionsAllowed == 0 {
				blocked = true
			}
			continue
		}
		if !sel.Matches(labels.Set(pod.Labels)) {
			continue
		}
		names = append(names, p.Name)
		if p.Status.DisruptionsAllowed == 0 {
			blocked = true
		}
	}
	return blocked, nil, names
}

func pdbMatchesWorkload(p *policyv1.PodDisruptionBudget, sel *metav1.LabelSelector) bool {
	if p.Spec.Selector == nil || sel == nil {
		return false
	}
	pdbSel, err := metav1.LabelSelectorAsSelector(p.Spec.Selector)
	if err != nil {
		return true
	}
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) > 0 {
		return true
	}
	return pdbSel.Matches(labels.Set(sel.MatchLabels))
}

func isControlPlanePod(p *corev1.Pod) bool {
	if p.Labels["label.kubernetes.io/control-plane"] != "" {
		return true
	}
	if p.Namespace == "kube-system" && (p.Labels["component"] == "kube-apiserver" || p.Labels["tier"] == "control-plane" || p.Labels["component"] == "etcd" || p.Labels["component"] == "kube-scheduler") {
		return true
	}
	return false
}

func isMirrorPod(p *corev1.Pod) bool {
	if p.Annotations["kubernetes.io/config.mirror"] != "" {
		return true
	}
	if p.Annotations["kubernetes.io/config.source"] == "file" || p.Annotations["kubernetes.io/config.source"] == "static" {
		return true
	}
	return false
}

func ownerKind(p *corev1.Pod) string {
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			return o.Kind
		}
	}
	if len(p.OwnerReferences) > 0 {
		return p.OwnerReferences[0].Kind
	}
	return ""
}

func inferRestartCategory(ctx context.Context, cs kubernetes.Interface, d *appsv1.Deployment) string {
	sel := ""
	if d.Spec.Selector != nil {
		sel = metav1.FormatLabelSelector(d.Spec.Selector)
	}
	list, err := cs.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil || list == nil {
		return ""
	}
	for i := range list.Items {
		p := &list.Items[i]
		if !graph.OwnedByUID(p, d.UID) {
			// may be owned by RS owned by d — check owner RS later; use waiting reason anyway only if hash matches
		}
		for _, cs := range p.Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil {
				switch w.Reason {
				case "ImagePullBackOff", "ErrImagePull":
					return "IMAGE_PULL_FAILURE"
				case "CreateContainerConfigError":
					return "CONFIG_ERROR"
				case "CrashLoopBackOff":
					return "CRASH_LOOP"
				}
			}
		}
	}
	return ""
}
