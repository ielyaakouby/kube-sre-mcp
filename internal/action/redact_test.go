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
	"fmt"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"kube-sre-mcp/internal/action"
	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/kube"
)

func TestActionAPIErrorRedactsSecrets(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	cs := fake.NewSimpleClientset()
	cs.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	cs.Fake.PrependReactor("get", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("Unauthorized: Bearer %s password=supersecret https://user:s3cret@api.example", jwt)
	})
	e := &action.Engine{
		Cfg:     config.Config{ActionsEnabled: true, ConfirmationTTL: time.Minute},
		Cluster: &kube.ClusterClient{Clientset: cs},
		Store:   action.NewStore(time.Minute),
	}
	r := e.RestartDeployment(context.Background(), "web", "ns", "", "", "", false)
	if r.Status != "error" {
		t.Fatalf("status: %#v", r)
	}
	if strings.Contains(r.Message, jwt) || strings.Contains(r.Message, "supersecret") || strings.Contains(r.Message, "s3cret") {
		t.Fatalf("sensitive material leaked: %s", r.Message)
	}
	if !strings.Contains(r.Message, "Unauthorized") {
		t.Fatalf("expected useful diagnostic text: %s", r.Message)
	}
}

func TestDisabledActionsIgnoreLegacyEnv(t *testing.T) {
	cs := fake.NewSimpleClientset()
	e := &action.Engine{
		Cfg:     config.Config{ActionsEnabled: false},
		Cluster: &kube.ClusterClient{Clientset: cs},
		Store:   action.NewStore(time.Minute),
	}
	r := e.RestartDeployment(context.Background(), "web", "ns", "", "", "", false)
	if r.Status != "disabled" {
		t.Fatalf("%#v", r)
	}
	if !strings.Contains(r.Message, "KUBE_SRE_MCP_ACTIONS_ENABLED") {
		t.Fatalf("message: %s", r.Message)
	}
}
