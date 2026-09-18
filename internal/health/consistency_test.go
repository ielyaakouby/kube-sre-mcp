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

package health_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/health"
	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func TestHealthConsistencyInventory(t *testing.T) {
	cases := []struct {
		name string
		obj  runtime.Object
		kind string
		want model.Health
	}{
		{"crash", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}}, "Pod", model.HealthCritical},
		{"ready", runningPod("r"), "Pod", model.HealthHealthy},
		{"pending", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pend", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending}}, "Pod", model.HealthCritical},
		{"node", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}}}, "Node", model.HealthCritical},
		{"pvc", &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pvc", Namespace: "default"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}, "PVC", model.HealthCritical},
	}
	for _, c := range cases {
		cs := fake.NewSimpleClientset(readyNode("ok"), c.obj)
		rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "", false, 50, nil)
		switch c.kind {
		case "Pod":
			if c.want == model.HealthHealthy && rep.Pods.Healthy != 1 {
				t.Fatalf("%s pods %#v", c.name, rep.Pods)
			}
			if c.want == model.HealthCritical && health.FromPod(c.obj.(*corev1.Pod)) != model.HealthCritical {
				t.Fatalf("%s FromPod", c.name)
			}
		case "Node":
			if health.FromNode(c.obj.(*corev1.Node)) != model.HealthCritical {
				t.Fatal("FromNode")
			}
		}
	}
	d0 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}}
	if health.FromCounts(3, 0, false) != model.HealthCritical {
		t.Fatal("0/3")
	}
	if health.FromCounts(3, 3, false) != model.HealthHealthy {
		t.Fatal("3/3")
	}
	if health.FromCounts(3, 2, false) != model.HealthDegraded {
		t.Fatal("sts 2/3")
	}
	_ = d0
}

func TestDeepDiagnosisPriorityAndTruncation(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "badnode"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}}}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "crit", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}}
	warnPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "warn", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 1, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "failjob", Namespace: "default"}, Status: batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pvc", Namespace: "default"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	cs := fake.NewSimpleClientset(n, d, warnPod, job, pvc, readyNode("ok"))
	var mu sync.Mutex
	var order []string
	deep := func(ctx context.Context, kind, name, namespace string) model.DiagnosticResponse {
		mu.Lock()
		order = append(order, kind+"/"+name)
		mu.Unlock()
		return model.DiagnosticResponse{Health: model.HealthCritical, RootCause: &model.RootCauseHypothesis{Category: "X"}}
	}
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{MaxClusterDeepDiagnoses: 3, MaxConcurrentK8s: 2}, "", false, 50, deep)
	if !rep.DeepDiagnosisTruncated {
		t.Fatalf("expected truncation skipped=%d order=%v", rep.DeepDiagnosisSkipped, order)
	}
	if len(order) != 3 {
		t.Fatalf("capped %v", order)
	}
	joined := strings.Join(order, ",")
	if !strings.Contains(joined, "Deployment/crit") {
		t.Fatalf("critical workload not selected %v", order)
	}
	if strings.Contains(joined, "Pod/warn") {
		t.Fatalf("spent cap on pod warning %v", order)
	}
}

func TestClusterListBudget(t *testing.T) {
	var objs []runtime.Object
	objs = append(objs, readyNode("n"))
	for i := 0; i < 200; i++ {
		objs = append(objs, runningPod("p"+strconv.Itoa(i)))
	}
	cs := fake.NewSimpleClientset(objs...)
	var lists int32
	cs.Fake.PrependReactor("list", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&lists, 1)
		return false, nil, nil
	})
	_ = health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{MaxClusterDeepDiagnoses: 2}, "", false, 20, nil)
	if lists > 5 {
		t.Fatalf("pod lists %d", lists)
	}
}
