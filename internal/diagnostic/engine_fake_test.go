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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/diagnostic"
	"kube-sre-mcp/internal/kube"
)

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func eng(objs ...runtime.Object) diagnostic.Engine {
	cs := kfake.NewSimpleClientset(objs...)
	dyn := fake.NewSimpleDynamicClient(scheme(), objs...)
	return diagnostic.Engine{Cfg: config.Config{MaxDeepPods: 8, MaxEvents: 20, MaxLogLines: 20, MaxGraphDepth: 6, MaxGraphNodes: 40, PrefixMatch: true}, Cluster: &kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default", ContextName: "test"}}
}

func TestEnginePendingUnscheduledCPU(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("1Gi")}}}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "x", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
	e := eng(n, p)
	r := e.Diagnose(context.Background(), "Pod", "p", "default")
	if r.RootCause == nil || r.RootCause.Category != "INSUFFICIENT_CPU" {
		t.Fatalf("got %#v health=%s", r.RootCause, r.Health)
	}
	if r.ScheduleMode == "events_only" {
		t.Fatal("should compute approximation")
	}
}

func TestEngineOwnerIsolation(t *testing.T) {
	dA := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "shared"}}, Replicas: int32Ptr(1)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 0}}
	dB := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default", UID: "db"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "shared"}}, Replicas: int32Ptr(1)}}
	rsA := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "a-rs", Namespace: "default", UID: "rsa", OwnerReferences: []metav1.OwnerReference{{UID: "da", Kind: "Deployment"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "shared"}}}}
	rsB := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "b-rs", Namespace: "default", UID: "rsb", OwnerReferences: []metav1.OwnerReference{{UID: "db", Kind: "Deployment"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "shared"}}}}
	podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b-pod", Namespace: "default", Labels: map[string]string{"app": "shared"}, OwnerReferences: []metav1.OwnerReference{{UID: "rsb", Kind: "ReplicaSet"}}}, Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "unauthorized"}}}}}}
	e := eng(dA, dB, rsA, rsB, podB)
	r := e.Diagnose(context.Background(), "Deployment", "a", "default")
	if r.RootCause != nil && r.RootCause.Category == "REGISTRY_AUTHENTICATION_FAILURE" {
		t.Fatalf("B's pods contaminated A: %#v", r.RootCause)
	}
}

func TestEngineSecret403NotMissing(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "c", Image: "x",
			Env: []corev1.EnvVar{{Name: "T", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}, Key: "k"}}}},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "c", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}},
	}
	e := eng(p)
	e.Cluster.Clientset.(*kfake.Clientset).Fake.PrependReactor("get", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "s", nil)
	})
	r := e.Diagnose(context.Background(), "Pod", "p", "default")
	if r.RootCause != nil && r.RootCause.Category == "MISSING_SECRET" {
		t.Fatal("403 must not mean missing secret")
	}
}

func TestEngineEndpointSlice403(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "x"}, Ports: []corev1.ServicePort{{Port: 80}}}}
	e := eng(svc)
	e.Cluster.Clientset.(*kfake.Clientset).Fake.PrependReactor("list", "endpointslices", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "endpointslices"}, "", nil)
	})
	r := e.Diagnose(context.Background(), "Service", "s", "default")
	if r.RootCause != nil && r.RootCause.Category == "SERVICE_NO_ENDPOINTS" {
		t.Fatal("403 must not mean no endpoints")
	}
	if r.Visibility == nil || !r.Visibility.Limited {
		t.Fatal("expected limited visibility")
	}
}

func TestOldRSDoesNotContaminate(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default", UID: "da"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}, Replicas: int32Ptr(1)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}}
	rsNew := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "default", UID: "new", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	rsOld := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default", UID: "old", OwnerReferences: []metav1.OwnerReference{{UID: "da"}}, Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "a"}}}}
	healthy := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "newp", Namespace: "default", Labels: map[string]string{"app": "a"}, OwnerReferences: []metav1.OwnerReference{{UID: "new"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	bad := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "oldp", Namespace: "default", Labels: map[string]string{"app": "a"}, OwnerReferences: []metav1.OwnerReference{{UID: "old"}}}, Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}}}
	e := eng(d, rsNew, rsOld, healthy, bad)
	r := e.Diagnose(context.Background(), "Deployment", "a", "default")
	if r.RootCause != nil && r.RootCause.Category == "CRASH_LOOP" {
		t.Fatal("old RS crash should not be current RCA")
	}
}

func int32Ptr(v int32) *int32 { return &v }

var _ = types.UID("")
