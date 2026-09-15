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

package config

import (
	"math"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Kubeconfig              string
	Context                 string
	LogLevel                string
	ActionsEnabled          bool
	MaxGraphDepth           int
	MaxGraphNodes           int
	MaxDeepPods             int
	MaxEvents               int
	MaxLogLines             int
	APITimeout              time.Duration
	DiagnosticTimeout       time.Duration
	MaxConcurrentK8s        int
	MaxReplicas             int32
	MetricsListen           string
	ConfirmationTTL         time.Duration
	PrefixMatch             bool
	MaxClusterDeepDiagnoses int
}

func Load() Config {
	return Config{
		Kubeconfig:              env("KUBECONFIG", ""),
		Context:                 env("KUBE_SRE_MCP_CONTEXT", ""),
		LogLevel:                env("KUBE_SRE_MCP_LOG_LEVEL", "info"),
		ActionsEnabled:          envBool("KUBE_SRE_MCP_ACTIONS_ENABLED", false),
		MaxGraphDepth:           envInt("KUBE_SRE_MCP_MAX_GRAPH_DEPTH", 6),
		MaxGraphNodes:           envInt("KUBE_SRE_MCP_MAX_GRAPH_NODES", 80),
		MaxDeepPods:             envInt("KUBE_SRE_MCP_MAX_DEEP_PODS", 8),
		MaxEvents:               envInt("KUBE_SRE_MCP_MAX_EVENTS", 50),
		MaxLogLines:             envInt("KUBE_SRE_MCP_MAX_LOG_LINES", 200),
		APITimeout:              time.Duration(envInt("KUBE_SRE_MCP_API_TIMEOUT_SECONDS", 15)) * time.Second,
		DiagnosticTimeout:       time.Duration(envInt("KUBE_SRE_MCP_DIAGNOSTIC_TIMEOUT", 45)) * time.Second,
		MaxConcurrentK8s:        envInt("KUBE_SRE_MCP_MAX_CONCURRENT_K8S", 8),
		MaxReplicas:             envInt32("KUBE_SRE_MCP_MAX_REPLICAS", 100),
		MetricsListen:           env("KUBE_SRE_MCP_METRICS_LISTEN", ""),
		ConfirmationTTL:         time.Duration(envInt("KUBE_SRE_MCP_CONFIRMATION_TTL_SECONDS", 120)) * time.Second,
		PrefixMatch:             envBool("KUBE_SRE_MCP_PREFIX_MATCH", true),
		MaxClusterDeepDiagnoses: envInt("KUBE_SRE_MCP_MAX_CLUSTER_DEEP_DIAGNOSES", 8),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt32(key string, def int32) int32 {
	n := envInt(key, int(def))
	if n < 0 {
		return def
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}
