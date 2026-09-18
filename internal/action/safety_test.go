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
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/action"
	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
)

func TestConfirmationTamperAndReplay(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns", UID: "uid-1"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 100, ConfirmationTTL: time.Minute}, Cluster: &kube.ClusterClient{Clientset: cs, ContextName: "a"}, Store: action.NewStore(time.Minute)}
	r := e.Scale(context.Background(), "Deployment", "foo", "ns", 3, "", "a")
	if r.Status != "confirmation_required" {
		t.Fatalf("%#v", r)
	}
	bad := e.Scale(context.Background(), "Deployment", "foo", "ns", 50, r.ConfirmationID, "a")
	if bad.Status != "confirmation_mismatch" {
		t.Fatalf("tamper replicas %#v", bad)
	}
	ns := e.Scale(context.Background(), "Deployment", "foo", "other", 3, r.ConfirmationID, "a")
	if ns.Status == "success" {
		t.Fatalf("ns tamper %#v", ns)
	}
	name := e.Scale(context.Background(), "Deployment", "bar", "ns", 3, r.ConfirmationID, "a")
	if name.Status == "success" {
		t.Fatalf("name tamper %#v", name)
	}
	ctx := e.Scale(context.Background(), "Deployment", "foo", "ns", 3, r.ConfirmationID, "b")
	if ctx.Status == "success" {
		t.Fatalf("context tamper %#v", ctx)
	}
	ok := e.Scale(context.Background(), "Deployment", "foo", "ns", 3, r.ConfirmationID, "a")
	if ok.Status != "success" {
		t.Fatalf("legit %#v", ok)
	}
	replay := e.Scale(context.Background(), "Deployment", "foo", "ns", 3, r.ConfirmationID, "a")
	if replay.Status == "success" {
		t.Fatal("replay must fail")
	}
}

func TestConfirmationExpiry(t *testing.T) {
	st := action.NewStore(10 * time.Millisecond)
	id, err := st.Put(action.Pending{Action: "scale", Kind: "Deployment", Name: "n", Namespace: "ns"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	_, err = st.Take(id, action.Pending{Action: "scale", Kind: "Deployment", Name: "n", Namespace: "ns"})
	if err == nil {
		t.Fatal("expected expiry")
	}
}

func TestConfirmationConcurrentTake(t *testing.T) {
	st := action.NewStore(time.Minute)
	id, err := st.Put(action.Pending{Action: "scale", Kind: "Deployment", Name: "n", Namespace: "ns"})
	if err != nil {
		t.Fatal(err)
	}
	var okCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := st.Take(id, action.Pending{Action: "scale", Kind: "Deployment", Name: "n", Namespace: "ns"})
			if e == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("got %d successful takes", okCount)
	}
}

func TestHPAMatchAnd403(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "h", Namespace: "ns"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "w"}}}
	other := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "o", Namespace: "ns"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "other"}}}
	cs := fake.NewSimpleClientset(d, hpa, other)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10}, Cluster: &kube.ClusterClient{Clientset: cs, ContextName: "c"}, Store: action.NewStore(time.Minute)}
	r := e.Scale(context.Background(), "Deployment", "w", "ns", 2, "", "")
	if r.Status != "confirmation_required" {
		t.Fatalf("hpa should require confirm %#v", r)
	}

	cs2 := fake.NewSimpleClientset(d)
	allowAll(cs2)
	cs2.Fake.PrependReactor("list", "horizontalpodautoscalers", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "horizontalpodautoscalers"}, "", nil)
	})
	e2 := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10}, Cluster: &kube.ClusterClient{Clientset: cs2}, Store: action.NewStore(time.Minute)}
	r2 := e2.Scale(context.Background(), "Deployment", "w", "ns", 2, "", "")
	if r2.Status != "preflight_failed" {
		t.Fatalf("hpa 403 %#v", r2)
	}
}

func TestPDBMatchAndBlock(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "w"}}, Replicas: int32Ptr(2)}, Status: appsv1.DeploymentStatus{AvailableReplicas: 2}}
	unrelated := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "zzz"}}}, Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0}}
	match0 := &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "ns"}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "w"}}}, Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0}}
	cs := fake.NewSimpleClientset(d, unrelated)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "w", "ns", "", "", "", false)
	if r.Status != "confirmation_required" {
		t.Fatalf("unrelated pdb %#v", r)
	}

	cs2 := fake.NewSimpleClientset(d, match0)
	allowAll(cs2)
	e2 := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs2}, Store: action.NewStore(time.Minute)}
	r2 := e2.RestartDeployment(context.Background(), "w", "ns", "", "", "", false)
	if r2.Status != "blocked_by_pdb" {
		t.Fatalf("pdb0 %#v", r2)
	}

	matchOK := match0.DeepCopy()
	matchOK.Status.DisruptionsAllowed = 1
	cs3 := fake.NewSimpleClientset(d, matchOK)
	allowAll(cs3)
	e3 := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs3}, Store: action.NewStore(time.Minute)}
	r3 := e3.RestartDeployment(context.Background(), "w", "ns", "", "", "", false)
	if r3.Status != "confirmation_required" {
		t.Fatalf("pdb allowed %#v", r3)
	}
}

func TestRestartNotRecommended(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "w", "ns", "", "REGISTRY_AUTHENTICATION_FAILURE", "", false)
	if r.Status != "restart_not_recommended" {
		t.Fatalf("%#v", r)
	}
}

func TestMirrorPodDeleteRefused(t *testing.T) {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "ns", Annotations: map[string]string{"kubernetes.io/config.mirror": "x"}}}
	cs := fake.NewSimpleClientset(p)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.DeletePod(context.Background(), "m", "ns", 30, "", "")
	if r.Status != "error" {
		t.Fatalf("%#v", r)
	}
}

func TestMultiContextWrite(t *testing.T) {
	dA := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns", UID: "a"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	dB := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns", UID: "b"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	csA := fake.NewSimpleClientset(dA)
	csB := fake.NewSimpleClientset(dB)
	allowAll(csA)
	allowAll(csB)
	a := &kube.ClusterClient{Clientset: csA, ContextName: "ctx-a"}
	b := &kube.ClusterClient{Clientset: csB, ContextName: "ctx-b"}
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10}, Cluster: a, Store: action.NewStore(time.Minute), ForContext: func(n string) (*kube.ClusterClient, error) {
		if n == "ctx-b" {
			return b, nil
		}
		return a, nil
	}}
	r := e.Scale(context.Background(), "Deployment", "w", "ns", 2, "", "ctx-a")
	if r.Status != "confirmation_required" {
		t.Fatalf("%#v", r)
	}
	mismatch := e.Scale(context.Background(), "Deployment", "w", "ns", 2, r.ConfirmationID, "ctx-b")
	if mismatch.Status != "confirmation_mismatch" {
		t.Fatalf("context bind %#v", mismatch)
	}
}

func TestSSARStillFirst(t *testing.T) {
	cs := fake.NewSimpleClientset()
	denyAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.Cordon(context.Background(), "n", "", "", true)
	if r.Status != "forbidden" {
		t.Fatalf("%#v", r)
	}
}

func TestActionsDisabled(t *testing.T) {
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: false}, Store: action.NewStore(time.Minute)}
	if e.Scale(context.Background(), "Deployment", "x", "ns", 1, "", "").Status != "disabled" {
		t.Fatal("expected disabled")
	}
}
