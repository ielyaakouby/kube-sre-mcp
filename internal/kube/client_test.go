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

package kube

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/version"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test
contexts:
- context:
    cluster: test
    namespace: ns-dev
    user: test
  name: dev
- context:
    cluster: test
    namespace: ns-prod
    user: test
  name: production
current-context: dev
users:
- name: test
  user:
    token: test-token
`

func writeKubeconfig(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolateKubeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("KUBECONFIG", "")
	_ = os.Unsetenv("KUBECONFIG")
	return home
}

func TestForContextValidKubeconfig(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "config", testKubeconfig)
	f := NewFactory(config.Config{Kubeconfig: path})
	cl, err := f.ForContext("")
	if err != nil {
		t.Fatalf("expected kubeconfig client: %v", err)
	}
	if cl.ContextName != "dev" {
		t.Fatalf("current-context: got %q", cl.ContextName)
	}
	if cl.Namespace != "ns-dev" {
		t.Fatalf("namespace: got %q", cl.Namespace)
	}
	if cl.RESTConfig == nil || cl.RESTConfig.Host != "https://127.0.0.1:6443" {
		t.Fatalf("server: %+v", cl.RESTConfig)
	}
	if cl.RESTConfig.UserAgent != version.UserAgent() {
		t.Fatalf("User-Agent: got %q want %q", cl.RESTConfig.UserAgent, version.UserAgent())
	}
}

func TestForContextKUBECONFIGEnv(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "env-config", testKubeconfig)
	t.Setenv("KUBECONFIG", path)
	f := NewFactory(config.Config{})
	cl, err := f.ForContext("")
	if err != nil {
		t.Fatalf("KUBECONFIG should load: %v", err)
	}
	if cl.ContextName != "dev" {
		t.Fatalf("got context %q", cl.ContextName)
	}
}

func TestForContextDefaultKubeconfig(t *testing.T) {
	home := isolateKubeHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".kube"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKubeconfig(t, filepath.Join(home, ".kube"), "config", testKubeconfig)
	f := NewFactory(config.Config{})
	cl, err := f.ForContext("")
	if err != nil {
		t.Fatalf("default ~/.kube/config should load: %v", err)
	}
	if cl.ContextName != "dev" {
		t.Fatalf("got context %q", cl.ContextName)
	}
}

func TestForContextExplicitContext(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "config", testKubeconfig)
	f := NewFactory(config.Config{Kubeconfig: path, Context: "production"})
	cl, err := f.ForContext("")
	if err != nil {
		t.Fatal(err)
	}
	if cl.ContextName != "production" {
		t.Fatalf("got %q", cl.ContextName)
	}
	if cl.Namespace != "ns-prod" {
		t.Fatalf("namespace %q", cl.Namespace)
	}
}

func TestForContextPerCallOverride(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "config", testKubeconfig)
	f := NewFactory(config.Config{Kubeconfig: path, Context: "dev"})
	cl, err := f.ForContext("production")
	if err != nil {
		t.Fatal(err)
	}
	if cl.ContextName != "production" {
		t.Fatalf("got %q", cl.ContextName)
	}
}

func TestForContextInvalidContext(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "config", testKubeconfig)
	f := NewFactory(config.Config{Kubeconfig: path})
	_, err := f.ForContext("missing-cluster")
	if err == nil {
		t.Fatal("expected error for unknown context")
	}
	msg := err.Error()
	if !strings.Contains(msg, `Kubernetes context "missing-cluster" was not found in the configured kubeconfig.`) {
		t.Fatalf("unexpected error: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "token") || strings.Contains(msg, "test-token") {
		t.Fatalf("error leaked credentials: %s", msg)
	}
}

func TestForContextMissingKubeconfig(t *testing.T) {
	isolateKubeHome(t)
	f := NewFactory(config.Config{})
	_, err := f.ForContext("")
	if err == nil {
		t.Fatal("expected startup failure without kubeconfig")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no usable kubeconfig found") {
		t.Fatalf("unexpected error: %s", msg)
	}
	if !strings.Contains(msg, "Set KUBECONFIG or configure ~/.kube/config") {
		t.Fatalf("missing hint: %s", msg)
	}
}

func TestForContextNoServiceAccountFallback(t *testing.T) {
	isolateKubeHome(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "127.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	f := NewFactory(config.Config{Kubeconfig: filepath.Join(t.TempDir(), "does-not-exist")})
	_, err := f.ForContext("")
	if err == nil {
		t.Fatal("kubeconfig failure must not fall back to ServiceAccount / in-cluster config")
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "in-cluster") || strings.Contains(msg, "serviceaccount") {
		t.Fatalf("must not mention in-cluster fallback: %s", err)
	}
	if !strings.Contains(err.Error(), "failed to initialize Kubernetes client") && !strings.Contains(err.Error(), "no usable kubeconfig found") {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestSourceDoesNotUseInClusterConfig(t *testing.T) {
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "InClusterConfig") {
		t.Fatal("rest.InClusterConfig must not be used for kube-sre-mcp authentication")
	}
}

func TestListKubeContextsUsesSameKubeconfig(t *testing.T) {
	isolateKubeHome(t)
	path := writeKubeconfig(t, t.TempDir(), "config", testKubeconfig)
	f := NewFactory(config.Config{Kubeconfig: path})
	cl, err := f.ForContext("")
	if err != nil {
		t.Fatal(err)
	}
	list := cl.ListKubeContexts()
	names := map[string]bool{}
	for _, item := range list {
		names[item["name"]] = true
	}
	if !names["dev"] || !names["production"] {
		t.Fatalf("contexts: %v", list)
	}
}

func TestKubeconfigLoadingRulesExplicitWindowsPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows list separator is required to keep C:\\ paths as a single kubeconfig file")
	}
	path := `C:\Users\example\.kube\config`
	rules := kubeconfigLoadingRules(path)
	if rules == nil {
		t.Fatal("nil loading rules")
	}
	if len(rules.Precedence) != 1 || rules.Precedence[0] != path {
		t.Fatalf("explicit Windows kubeconfig path: %#v", rules.Precedence)
	}
}

func TestKubeconfigLoadingRulesExplicitUnixPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path separator semantics")
	}
	path := "/Users/example/.kube/config"
	rules := kubeconfigLoadingRules(path)
	if len(rules.Precedence) != 1 || rules.Precedence[0] != path {
		t.Fatalf("explicit kubeconfig path: %#v", rules.Precedence)
	}
}

func TestDefaultKubeconfigUsesHomeDir(t *testing.T) {
	home := isolateKubeHome(t)
	rules := kubeconfigLoadingRules("")
	want := filepath.Join(home, ".kube", "config")
	found := false
	for _, p := range rules.Precedence {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected default kubeconfig %q in %#v", want, rules.Precedence)
	}
}
