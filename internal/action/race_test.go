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

package action_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/action"
	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
)

func TestUIDRecreateRace(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns", UID: "uid-a"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10, ConfirmationTTL: time.Minute}, Cluster: &kube.ClusterClient{Clientset: cs, ContextName: "c"}, Store: action.NewStore(time.Minute)}
	r := e.Scale(context.Background(), "Deployment", "foo", "ns", 2, "", "c")
	if r.Status != "confirmation_required" {
		t.Fatalf("%#v", r)
	}
	var gets int32
	cs.Fake.PrependReactor("get", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		n := atomic.AddInt32(&gets, 1)
		uid := types.UID("uid-a")
		if n >= 2 {
			uid = "uid-b"
		}
		return true, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns", UID: uid}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}, nil
	})
	out := e.Scale(context.Background(), "Deployment", "foo", "ns", 2, r.ConfirmationID, "c")
	if out.Status == "success" {
		t.Fatalf("must not mutate recreated uid %#v", out)
	}
}

func TestHPAAppearsAfterConfirm(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns", UID: "u1"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10, ConfirmationTTL: time.Minute}, Cluster: &kube.ClusterClient{Clientset: cs, ContextName: "c"}, Store: action.NewStore(time.Minute)}
	r := e.Scale(context.Background(), "Deployment", "w", "ns", 3, "", "c")
	if r.Status != "confirmation_required" {
		t.Fatalf("%#v", r)
	}
	_, _ = cs.AutoscalingV2().HorizontalPodAutoscalers("ns").Create(context.Background(), &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "h", Namespace: "ns"},
		Spec:       autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "w"}},
	}, metav1.CreateOptions{})
	out := e.Scale(context.Background(), "Deployment", "w", "ns", 3, r.ConfirmationID, "c")
	if out.Status == "success" {
		t.Fatalf("hpa after confirm %#v", out)
	}
}

func TestPDBMatchExpressionsAndExecutionRecheck(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns", UID: "u"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "w"}}, Replicas: int32Ptr(2)}}
	pdb := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "ns"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"w"}}}}}, Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 1}}
	cs := fake.NewSimpleClientset(d, pdb)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, ConfirmationTTL: time.Minute}, Cluster: &kube.ClusterClient{Clientset: cs, ContextName: "c"}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "w", "ns", "", "", "c", false)
	if r.Status != "confirmation_required" {
		t.Fatalf("matchExpressions %#v", r)
	}
	cs.Fake.PrependReactor("list", "poddisruptionbudgets", func(a ktesting.Action) (bool, runtime.Object, error) {
		p := pdb.DeepCopy()
		p.Status.DisruptionsAllowed = 0
		return true, &policyv1.PodDisruptionBudgetList{Items: []policyv1.PodDisruptionBudget{*p}}, nil
	})
	out := e.RestartDeployment(context.Background(), "w", "ns", r.ConfirmationID, "", "c", false)
	if out.Status != "blocked_by_pdb" {
		t.Fatalf("exec recheck %#v", out)
	}
}

func TestExactNameRequiredForWrites(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "website", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "web", "ns", "", "", "", false)
	if r.Status == "confirmation_required" || r.Status == "success" {
		t.Fatalf("prefix must not select write target %#v", r)
	}
}
