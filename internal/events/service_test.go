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

package events_test

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/events"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func ev(id, obj, kind, reason, msg, typ string, count int32, ns string) *corev1.Event {
	now := metav1.NewTime(time.Now())
	first := metav1.NewTime(time.Now().Add(-time.Hour))
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: id, Namespace: ns},
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: obj, Namespace: ns},
		Reason:         reason, Message: msg, Type: typ, Count: count,
		FirstTimestamp: first, LastTimestamp: now,
	}
}

func TestServiceListAggregateAndCases(t *testing.T) {
	items := []runtime.Object{
		ev("e1", "p", "Pod", "BackOff", "Back-off restarting failed container", "Warning", 2, "default"),
		ev("e2", "p", "Pod", "BackOff", "Back-off restarting failed container", "Warning", 3, "default"),
		ev("e3", "p", "Pod", "BackOff", "Back-off pulling image nginx", "Warning", 1, "default"),
		ev("e4", "p", "Pod", "Unhealthy", "Readiness probe failed", "Warning", 1, "default"),
		ev("e5", "p", "Pod", "FailedMount", "mount timeout", "Warning", 1, "default"),
		ev("e6", "p", "Pod", "FailedAttachVolume", "attach failed", "Warning", 1, "default"),
		ev("e7", "p", "Pod", "FailedScheduling", "0/3 nodes", "Warning", 1, "default"),
		ev("e8", "n1", "Node", "NodeNotReady", "not ready", "Warning", 1, "default"),
	}
	cs := fake.NewSimpleClientset(items...)
	svc := events.Service{CS: cs}
	list, err := svc.List(context.Background(), "default", "p", "Pod", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	var crash, image bool
	var first, last time.Time
	var count int32
	for _, r := range list {
		if r.Reason == "BackOff" && r.Category == "Container" {
			crash = true
			count = r.Count
			first, last = r.FirstTime, r.LastTime
		}
		if r.Reason == "BackOff" && r.Category == "Image" {
			image = true
		}
	}
	if !crash || !image {
		t.Fatalf("backoff cases %#v", list)
	}
	if count < 5 {
		t.Fatalf("aggregated count %d", count)
	}
	if last.Before(first) {
		t.Fatal("ordering first/last")
	}
	sigs := events.ToSignals(model.ResourceRef{Kind: "Pod", Name: "p"}, list)
	if len(sigs) == 0 {
		t.Fatal("signals")
	}

	nodeList, err := svc.List(context.Background(), "", "n1", "Node", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	_ = nodeList

	cs2 := fake.NewSimpleClientset()
	cs2.Fake.PrependReactor("list", "events", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", nil)
	})
	svc2 := events.Service{CS: cs2}
	_, err = svc2.List(context.Background(), "default", "p", "Pod", false, 10)
	if err == nil {
		t.Fatal("403")
	}

	cs3 := fake.NewSimpleClientset()
	cs3.Fake.PrependReactor("list", "events", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.DeadlineExceeded
	})
	svc3 := events.Service{CS: cs3}
	_, err = svc3.List(context.Background(), "default", "p", "Pod", false, 10)
	if err == nil {
		t.Fatal("timeout")
	}
}
