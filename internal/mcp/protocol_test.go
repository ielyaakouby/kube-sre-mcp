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

package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/kube"
	mcpserver "kube-sre-mcp/internal/mcp"
	"kube-sre-mcp/internal/observability"
)

func schemeAll() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func mcpSession(t *testing.T, cfg config.Config, objs ...runtime.Object) *mcp.ClientSession {
	t.Helper()
	cs := kfake.NewSimpleClientset(objs...)
	cs.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	dyn := fake.NewSimpleDynamicClient(schemeAll(), objs...)
	cl := &kube.ClusterClient{Clientset: cs, Dynamic: dyn, Namespace: "default", ContextName: "test", Server: "https://kube.example"}
	s := mcpserver.New(cfg, cl, nil, observability.NewMetrics())
	srv := mcp.NewServer(&mcp.Implementation{Name: "Kube SRE MCP", Version: "test"}, nil)
	s.Register(srv)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "test"}, nil)
	sess, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func callJSON(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s empty content", name)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content %T", res.Content[0])
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("json: %v %s", err, tc.Text)
	}
	return m
}

func TestMCPProtocolFlow(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pay", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "ok", Image: "x"}, {Name: "crash", Image: "y"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
			{Name: "ok", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			{Name: "crash", Ready: false, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
		}},
	}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default"}, Data: map[string][]byte{"password": []byte("hunter2")}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default", UID: "d1"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}}}
	sess := mcpSession(t, config.Config{ActionsEnabled: true, PrefixMatch: true, DiagnosticTimeout: time.Second, APITimeout: time.Second, MaxEvents: 10, MaxLogLines: 20, MaxDeepPods: 4, MaxGraphDepth: 4, MaxGraphNodes: 20, ConfirmationTTL: time.Minute, MaxReplicas: 10}, pod, sec, dep)

	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 18 {
		t.Fatalf("tools %d", len(tools.Tools))
	}
	seen := map[string]struct{}{}
	var writes, reads int
	for _, tl := range tools.Tools {
		if _, ok := seen[tl.Name]; ok {
			t.Fatalf("dup %s", tl.Name)
		}
		seen[tl.Name] = struct{}{}
		if tl.Annotations == nil {
			t.Fatalf("missing annotations %s", tl.Name)
		}
		if strings.HasPrefix(tl.Name, "k8s_restart") || strings.HasPrefix(tl.Name, "k8s_scale") || strings.HasPrefix(tl.Name, "k8s_delete") || strings.HasPrefix(tl.Name, "k8s_cordon") || strings.HasPrefix(tl.Name, "k8s_uncordon") {
			if tl.Annotations.ReadOnlyHint {
				t.Fatalf("write marked read-only %s", tl.Name)
			}
			writes++
			if tl.Name == "k8s_delete_pod" && (tl.Annotations.DestructiveHint == nil || !*tl.Annotations.DestructiveHint) {
				t.Fatal("delete should be destructive")
			}
		} else {
			if !tl.Annotations.ReadOnlyHint {
				t.Fatalf("read not read-only %s", tl.Name)
			}
			if tl.Annotations.DestructiveHint != nil && *tl.Annotations.DestructiveHint {
				t.Fatalf("read marked destructive %s", tl.Name)
			}
			reads++
		}
	}
	if writes != 5 || reads != 13 {
		t.Fatalf("writes=%d reads=%d", writes, reads)
	}

	ctx := callJSON(t, sess, "k8s_get_context", map[string]any{})
	if ctx["context"] != "test" {
		t.Fatalf("context %#v", ctx)
	}

	got := callJSON(t, sess, "k8s_get_resource", map[string]any{"kind": "Secret", "name": "s", "namespace": "default"})
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), `"data"`) {
		t.Fatalf("secret data leaked %s", raw)
	}

	diag := callJSON(t, sess, "k8s_diagnose_resource", map[string]any{"kind": "Pod", "name": "pay", "namespace": "default"})
	if diag["status"] == nil {
		t.Fatalf("diagnose %#v", diag)
	}

	missing := callJSON(t, sess, "k8s_diagnose_resource", map[string]any{"kind": "Pod", "name": "no-such-pod", "namespace": "default"})
	st, _ := missing["status"].(string)
	if st != "not_found" && st != "error" {
		t.Fatalf("unknown resource %#v", missing)
	}

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sess.CallTool(cctx, &mcp.CallToolParams{Name: "k8s_list_resources", Arguments: map[string]any{"kind": "Pod"}})
	if err == nil {
		t.Fatal("expected cancelled context error")
	}

	conf := callJSON(t, sess, "k8s_restart_deployment", map[string]any{"name": "web", "namespace": "default"})
	if conf["status"] != "confirmation_required" {
		t.Fatalf("confirm %#v", conf)
	}

	logs := callJSON(t, sess, "k8s_get_logs", map[string]any{"pod": "pay", "namespace": "default"})
	if logs["container"] != "crash" {
		t.Fatalf("failing container %#v", logs)
	}
	logsA := callJSON(t, sess, "k8s_get_logs", map[string]any{"pod": "pay", "namespace": "default", "container": "ok"})
	if logsA["container"] != "ok" {
		t.Fatalf("explicit container %#v", logsA)
	}
}

func TestMCPActionsDisabledAndInvalidArgs(t *testing.T) {
	sess := mcpSession(t, config.Config{ActionsEnabled: false, DiagnosticTimeout: time.Second})
	dis := callJSON(t, sess, "k8s_scale_workload", map[string]any{"kind": "Deployment", "name": "x", "namespace": "ns", "replicas": 2})
	if dis["status"] != "disabled" {
		t.Fatalf("disabled %#v", dis)
	}
	_, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "k8s_diagnose_resource", Arguments: map[string]any{}})
	if err == nil {
		res := callJSON(t, sess, "k8s_diagnose_resource", map[string]any{"kind": "", "name": ""})
		if res["status"] == "success" {
			t.Fatal("expected schema/invalid arguments error")
		}
	}
}

func int32Ptr(v int32) *int32 { return &v }
