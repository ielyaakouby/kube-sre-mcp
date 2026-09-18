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

package config_test

import (
	"os"
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
)

func TestActionsDisabledByDefault(t *testing.T) {
	t.Setenv("KUBE_SRE_MCP_ACTIONS_ENABLED", "")
	_ = os.Unsetenv("KUBE_SRE_MCP_ACTIONS_ENABLED")
	cfg := config.Load()
	if cfg.ActionsEnabled {
		t.Fatal("writes must default to disabled")
	}
}

func TestActionsExplicitTrue(t *testing.T) {
	t.Setenv("KUBE_SRE_MCP_ACTIONS_ENABLED", "true")
	cfg := config.Load()
	if !cfg.ActionsEnabled {
		t.Fatal("explicit true")
	}
}

func TestLegacyK8SPrefixDoesNotEnableActions(t *testing.T) {
	t.Setenv("KUBE_SRE_MCP_ACTIONS_ENABLED", "")
	_ = os.Unsetenv("KUBE_SRE_MCP_ACTIONS_ENABLED")
	t.Setenv("K8S_MCP_ACTIONS_ENABLED", "true")
	cfg := config.Load()
	if cfg.ActionsEnabled {
		t.Fatal("K8S_MCP_ACTIONS_ENABLED must not enable actions; use KUBE_SRE_MCP_ACTIONS_ENABLED")
	}
}

func TestMaxReplicasClampsToInt32(t *testing.T) {
	t.Setenv("KUBE_SRE_MCP_MAX_REPLICAS", "3000000000")
	cfg := config.Load()
	if cfg.MaxReplicas != 2147483647 {
		t.Fatalf("MaxReplicas: got %d want 2147483647", cfg.MaxReplicas)
	}
}
