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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/homedir"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/ielyaakouby/kube-sre-mcp/internal/config"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/security"
	"github.com/ielyaakouby/kube-sre-mcp/internal/version"
)

const noKubeconfigHint = "failed to initialize Kubernetes client: no usable kubeconfig found\n\nSet KUBECONFIG or configure ~/.kube/config before starting kube-sre-mcp."

// ClusterClient is a reusable, non-singleton Kubernetes access object.
type ClusterClient struct {
	Clientset      kubernetes.Interface
	Dynamic        dynamic.Interface
	Discovery      discovery.DiscoveryInterface
	RESTMapper     meta.RESTMapper
	Metrics        metricsclient.Interface
	RESTConfig     *rest.Config
	ContextName    string
	ClusterName    string
	Server         string
	Namespace      string
	kubeconfigPath string
}

func (c *ClusterClient) Info() model.ClusterInfo {
	return model.ClusterInfo{
		Context:   c.ContextName,
		Server:    c.Server,
		Namespace: c.Namespace,
		Cluster:   c.ClusterName,
	}
}

type Factory struct {
	cfg config.Config
}

func NewFactory(cfg config.Config) *Factory {
	return &Factory{cfg: cfg}
}

func (f *Factory) ForContext(ctxName string) (*ClusterClient, error) {
	loading := kubeconfigLoadingRules(f.cfg.Kubeconfig)
	overrides := &clientcmd.ConfigOverrides{}
	requested := ctxName
	if requested == "" {
		requested = f.cfg.Context
	}
	if requested != "" {
		overrides.CurrentContext = requested
	}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides)

	raw, err := clientCfg.RawConfig()
	if err != nil {
		return nil, wrapKubeconfigError(err, requested)
	}
	if err := validateKubeconfig(raw, requested); err != nil {
		return nil, err
	}

	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, wrapKubeconfigError(err, requested)
	}
	restCfg.Timeout = f.cfg.APITimeout
	restCfg.UserAgent = version.UserAgent()

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %s", security.Redact(err.Error()))
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %s", security.Redact(err.Error()))
	}
	disco := cs.Discovery()
	gr, err := restmapper.GetAPIGroupResources(disco)
	var mapper meta.RESTMapper
	if err == nil {
		mapper = restmapper.NewDiscoveryRESTMapper(gr)
	} else {
		mapper = restmapper.NewShortcutExpander(meta.NewDefaultRESTMapper(nil), disco, nil)
	}
	mc, _ := metricsclient.NewForConfig(restCfg)

	ns, _, _ := clientCfg.Namespace()
	if ns == "" {
		ns = "default"
	}
	current := raw.CurrentContext
	if requested != "" {
		current = requested
	}
	clusterName := ""
	server := restCfg.Host
	if c, ok := raw.Contexts[current]; ok && c != nil {
		clusterName = c.Cluster
		if cl, ok := raw.Clusters[c.Cluster]; ok && cl != nil && cl.Server != "" {
			server = cl.Server
		}
		if ns == "default" && c.Namespace != "" {
			ns = c.Namespace
		}
	}
	return &ClusterClient{
		Clientset:      cs,
		Dynamic:        dyn,
		Discovery:      disco,
		RESTMapper:     mapper,
		Metrics:        mc,
		RESTConfig:     restCfg,
		ContextName:    current,
		ClusterName:    clusterName,
		Server:         server,
		Namespace:      ns,
		kubeconfigPath: f.cfg.Kubeconfig,
	}, nil
}

func (c *ClusterClient) ListKubeContexts() []map[string]string {
	loading := kubeconfigLoadingRules(c.kubeconfigPath)
	apiCfg, err := loading.Load()
	if err != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(apiCfg.Contexts))
	for name, ctx := range apiCfg.Contexts {
		item := map[string]string{"name": name}
		if ctx != nil {
			item["cluster"] = ctx.Cluster
			item["user"] = ctx.AuthInfo
			item["namespace"] = ctx.Namespace
		}
		out = append(out, item)
	}
	return out
}

func WithTimeout(parent context.Context, cfg config.Config) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, cfg.DiagnosticTimeout)
}

func kubeconfigLoadingRules(explicit string) *clientcmd.ClientConfigLoadingRules {
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	loading.MigrationRules = map[string]string{}
	if explicit != "" {
		loading.ExplicitPath = ""
		loading.Precedence = filepath.SplitList(explicit)
		return loading
	}
	// Recompute ~/.kube/config from the current HOME. client-go snapshots
	// RecommendedHomeFile at package init, which is wrong for tests that
	// isolate HOME and for processes that inherit a stale snapshot.
	if os.Getenv(clientcmd.RecommendedConfigPathEnvVar) == "" {
		loading.Precedence = []string{filepath.Join(homedir.HomeDir(), clientcmd.RecommendedHomeDir, clientcmd.RecommendedFileName)}
	}
	return loading
}

func validateKubeconfig(raw clientcmdapi.Config, requested string) error {
	if len(raw.Contexts) == 0 {
		return fmt.Errorf("%s", noKubeconfigHint)
	}
	if requested != "" {
		if _, ok := raw.Contexts[requested]; !ok {
			return fmt.Errorf("Kubernetes context %q was not found in the configured kubeconfig.", requested)
		}
		return nil
	}
	if raw.CurrentContext == "" {
		return fmt.Errorf("%s", noKubeconfigHint)
	}
	if _, ok := raw.Contexts[raw.CurrentContext]; !ok {
		return fmt.Errorf("Kubernetes context %q was not found in the configured kubeconfig.", raw.CurrentContext)
	}
	return nil
}

func wrapKubeconfigError(err error, requested string) error {
	if err == nil {
		return fmt.Errorf("%s", noKubeconfigHint)
	}
	if requested != "" && clientcmd.IsContextNotFound(err) {
		return fmt.Errorf("Kubernetes context %q was not found in the configured kubeconfig.", requested)
	}
	if clientcmd.IsEmptyConfig(err) || os.IsNotExist(err) {
		return fmt.Errorf("%s", noKubeconfigHint)
	}
	return fmt.Errorf("failed to initialize Kubernetes client: %s", security.Redact(err.Error()))
}
