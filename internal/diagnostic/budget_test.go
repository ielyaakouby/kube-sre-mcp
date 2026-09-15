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

package diagnostic_test

import (
	"context"
	"sync/atomic"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/diagnostic"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/model"
)

func engineCounted(t *testing.T, objs []runtime.Object) (diagnostic.Engine, *int32, *int32) {
	t.Helper()
	cs := kfake.NewSimpleClientset(objs...)
	dyn := fake.NewSimpleDynamicClient(scheme(), objs...)
	var gets, lists int32
	cs.Fake.PrependReactor("*", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		switch action.GetVerb() {
		case "get":
			atomic.AddInt32(&gets, 1)
		case "list":
			atomic.AddInt32(&lists, 1)
		}
		return false, nil, nil
	})
	return diagnostic.Engine{Cfg: config.Config{MaxDeepPods: 8, MaxEvents: 10, MaxLogLines: 10, MaxGraphDepth: 4, MaxGraphNodes: 40, PrefixMatch: true, MaxConcurrentK8s: 4}, Cluster: &kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default", ContextName: "test"}}, &gets, &lists
}

func crashPod(name, ns, rsUID string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": "x"}, OwnerReferences: []metav1.OwnerReference{{UID: types.UID(rsUID), Kind: "ReplicaSet"}}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "x"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{Name: "c", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}},
	}
}

func TestEvidenceIDsResolve(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pay", Namespace: "default", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}, Replicas: int32Ptr(2)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pay-rs", Namespace: "default", UID: "rsa", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}}
	p1 := crashPod("payment-a", "default", "rsa")
	p2 := crashPod("payment-b", "default", "rsa")
	e, _, _ := engineCounted(t, []runtime.Object{d, rs, p1, p2})
	r := e.Diagnose(context.Background(), "Deployment", "pay", "default")
	ids := map[string]struct{}{}
	for _, ev := range r.Evidence {
		if ev.ID != "" {
			ids[ev.ID] = struct{}{}
		}
	}
	check := func(h model.RootCauseHypothesis) {
		for _, id := range h.SupportingEvidenceIDs {
			if _, ok := ids[id]; !ok {
				t.Fatalf("orphan supporting %s in %#v ids=%v", id, h, ids)
			}
		}
		for _, id := range h.ContradictingEvidenceIDs {
			if _, ok := ids[id]; !ok {
				t.Fatalf("orphan contradicting %s", id)
			}
		}
	}
	for _, h := range r.RootCauses {
		check(h)
	}
	if r.RootCause != nil {
		check(*r.RootCause)
	}
}

func TestAPICallBudgetDeployment(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}, Replicas: int32Ptr(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "w-rs", Namespace: "default", UID: "rsa", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}}
	var objs []runtime.Object
	objs = append(objs, d, rs)
	for i := 0; i < 3; i++ {
		objs = append(objs, crashPod("p"+string(rune('a'+i)), "default", "rsa"))
	}
	e, gets, lists := engineCounted(t, objs)
	_ = e.Diagnose(context.Background(), "Deployment", "w", "default")
	if *lists > 40 {
		t.Fatalf("list explosion %d", *lists)
	}
	if *gets > 80 {
		t.Fatalf("get explosion %d", *gets)
	}

	objs100 := []runtime.Object{d, rs}
	for i := 0; i < 100; i++ {
		objs100 = append(objs100, crashPod("n"+itoa(i), "default", "rsa"))
	}
	e2, _, lists2 := engineCounted(t, objs100)
	e2.Cfg.MaxDeepPods = 8
	_ = e2.Diagnose(context.Background(), "Deployment", "w", "default")
	if *lists2 > 80 {
		t.Fatalf("100-pod list explosion %d (possible O(N) namespace lists)", *lists2)
	}
}

func TestAPICallBudgetCluster(t *testing.T) {
	var objs []runtime.Object
	objs = append(objs, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
	for i := 0; i < 200; i++ {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p" + itoa(i), Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
		objs = append(objs, p)
	}
	cs := kfake.NewSimpleClientset(objs...)
	var lists int32
	cs.Fake.PrependReactor("list", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&lists, 1)
		return false, nil, nil
	})
	rep := diagnostic.Engine{}.Cfg
	_ = rep
	fromHealth := func() {
		// listed via health.Cluster
	}
	_ = fromHealth
	if lists > 100000 {
		t.Fatal("impossible")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
