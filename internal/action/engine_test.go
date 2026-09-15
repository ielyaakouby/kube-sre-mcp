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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"kube-sre-mcp/internal/action"
	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/kube"
)

func allowAll(cs *fake.Clientset) {
	cs.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
}

func denyAll(cs *fake.Clientset) {
	cs.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: false, Reason: "nope"}}, nil
	})
}

func TestSSARDenied(t *testing.T) {
	cs := fake.NewSimpleClientset()
	denyAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 50, ConfirmationTTL: time.Minute}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "x", "ns", "", "", "", false)
	if r.Status != "forbidden" {
		t.Fatalf("%#v", r)
	}
}

func TestConfirmationAndLastNode(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(3)}, Status: appsv1.DeploymentStatus{AvailableReplicas: 1}}
	cs := fake.NewSimpleClientset(d)
	allowAll(cs)
	e := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 50}, Cluster: &kube.ClusterClient{Clientset: cs}, Store: action.NewStore(time.Minute)}
	r := e.RestartDeployment(context.Background(), "x", "ns", "", "", "", false)
	if r.Status != "confirmation_required" || r.ConfirmationID == "" {
		t.Fatalf("%#v", r)
	}
	r2 := e.RestartDeployment(context.Background(), "x", "ns", r.ConfirmationID, "", "", false)
	if r2.Status != "success" {
		t.Fatalf("%#v", r2)
	}

	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "only"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	cs2 := fake.NewSimpleClientset(n)
	allowAll(cs2)
	e2 := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs2}, Store: action.NewStore(time.Minute)}
	c := e2.Cordon(context.Background(), "only", "", "", true)
	if c.Status != "error" || c.Risk != action.RiskCritical {
		t.Fatalf("last node %#v", c)
	}

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "lonely", Namespace: "ns"}}
	cs3 := fake.NewSimpleClientset(pod)
	allowAll(cs3)
	e3 := &action.Engine{Cfg: config.Config{ActionsEnabled: true}, Cluster: &kube.ClusterClient{Clientset: cs3}, Store: action.NewStore(time.Minute)}
	dp := e3.DeletePod(context.Background(), "lonely", "ns", 0, "", "")
	if dp.Status != "confirmation_required" {
		t.Fatalf("unmanaged %#v", dp)
	}

	hpa := false
	_ = hpa
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "ns"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}}
	cs4 := fake.NewSimpleClientset(dep)
	allowAll(cs4)
	e4 := &action.Engine{Cfg: config.Config{ActionsEnabled: true, MaxReplicas: 10}, Cluster: &kube.ClusterClient{Clientset: cs4}, Store: action.NewStore(time.Minute)}
	sc := e4.Scale(context.Background(), "Deployment", "w", "ns", 8, "", "")
	if sc.Status != "confirmation_required" {
		t.Fatalf("scale %#v", sc)
	}
}

func int32Ptr(v int32) *int32 { return &v }
