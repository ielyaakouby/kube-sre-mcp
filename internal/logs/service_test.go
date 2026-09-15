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

package logs_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"kube-sre-mcp/internal/logs"
	"kube-sre-mcp/internal/model"
)

func logServer(t *testing.T, body string, status int) kubernetes.Interface {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/log") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestServiceGetLogs(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.abc"
	cs := logServer(t, "line1\npassword=hunter2\nBearer "+jwt+"\n", 200)
	svc := logs.Service{CS: cs}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tail := int64(2)
	res := svc.Get(ctx, "p", "default", "crash", false, tail, nil, true)
	if res.Error != "" && len(res.Lines) == 0 {
		t.Log(res.Error)
	}
	joined := strings.Join(res.Lines, "\n")
	if strings.Contains(joined, "hunter2") || strings.Contains(joined, jwt) {
		t.Fatalf("redaction failed %q", joined)
	}
	if res.Container != "crash" || res.Previous {
		t.Fatalf("%#v", res)
	}

	csErr := logServer(t, "nope", 500)
	errSvc := logs.Service{CS: csErr}
	errRes := errSvc.Get(ctx, "p", "default", "c", true, 10, nil, false)
	if errRes.Error == "" {
		t.Fatal("expected stream error")
	}
	if !errRes.Previous {
		t.Fatal("previous")
	}

	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	canc := svc.Get(cctx, "p", "default", "c", false, 10, nil, true)
	if canc.Error == "" && len(canc.Lines) == 0 {
		t.Log("cancellation surfaced as empty or error")
	}
}

func TestSelectFailingAndFalsePositives(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "ok"}, {Name: "crash"}},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "ok", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "crash", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
			},
		},
	}
	if logs.SelectFailingContainer(p) != "crash" {
		t.Fatal(logs.SelectFailingContainer(p))
	}
	p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
	if logs.SelectFailingContainer(p) != "init" {
		t.Fatal("init preferred when failing")
	}
	fp := logs.Analyze(model.ResourceRef{Kind: "Pod", Name: "p"}, "app", []string{
		"GET /login HTTP 403",
		"HTTP 401 unauthorized for user session",
		"context deadline exceeded waiting for upstream",
		"Failed to pull image: unauthorized: authentication required",
	})
	for _, s := range fp {
		if strings.Contains(strings.ToLower(s.Message), "http 403") && s.Category != "Application" {
			t.Fatalf("%#v", s)
		}
	}
}
