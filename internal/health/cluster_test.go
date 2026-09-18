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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/health"
	"github.com/ielyaakouby/kube-sre-mcp/internal/kube"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func readyNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

func runningPod(name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "c", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
}

func TestClusterHealthyVisible(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("n1"), runningPod("p1"))
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs, ContextName: "c"}, config.Config{}, "", false, 50, nil)
	if rep.Health != model.HealthHealthy {
		t.Fatalf("%s vis=%#v", rep.Health, rep.Visibility)
	}
}

func TestClusterDeploy403NotHealthy(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("n1"), runningPod("p1"))
	cs.Fake.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "", nil)
	})
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "", false, 50, nil)
	if rep.Health == model.HealthHealthy {
		t.Fatalf("403 must not be healthy: %#v", rep)
	}
	if rep.Health != model.HealthUnknown {
		t.Fatalf("want unknown got %s issues=%v vis=%#v", rep.Health, rep.CriticalIssues, rep.Visibility)
	}
}

func TestClusterSTSTimeoutUnknown(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("n1"), runningPod("p1"))
	cs.Fake.PrependReactor("list", "statefulsets", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "", false, 50, nil)
	if rep.Health == model.HealthHealthy {
		t.Fatal(rep.Health)
	}
}

func TestClusterCriticalPlusVisibility(t *testing.T) {
	n := readyNode("n1")
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	cs := fake.NewSimpleClientset(n)
	cs.Fake.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "", nil)
	})
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "", false, 50, nil)
	if rep.Health != model.HealthCritical {
		t.Fatalf("got %s", rep.Health)
	}
	if rep.Visibility == nil || !rep.Visibility.Limited {
		t.Fatal("expected limited visibility")
	}
}

func TestClusterEmptyNamespaceHealthy(t *testing.T) {
	cs := fake.NewSimpleClientset(readyNode("n1"))
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "empty", false, 50, nil)
	if rep.Health != model.HealthHealthy {
		t.Fatalf("%s %#v", rep.Health, rep)
	}
}

func TestPodHealthConsistency(t *testing.T) {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"}, Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}}
	if health.FromPod(p) != model.HealthCritical {
		t.Fatal(health.FromPod(p))
	}
}

func TestClusterIncludeHealthy(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	cs := fake.NewSimpleClientset(readyNode("n1"), runningPod("p1"), d)
	rep := health.Cluster(context.Background(), &kube.ClusterClient{Clientset: cs}, config.Config{}, "default", true, 50, nil)
	if len(rep.HealthyResources) == 0 {
		t.Fatal("include_healthy should list healthy resources")
	}
}

func int32Ptr(v int32) *int32 { return &v }
