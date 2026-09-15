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

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"kube-sre-mcp/internal/action"
	"kube-sre-mcp/internal/config"
	"kube-sre-mcp/internal/diagnostic"
	"kube-sre-mcp/internal/events"
	"kube-sre-mcp/internal/health"
	"kube-sre-mcp/internal/kube"
	"kube-sre-mcp/internal/logs"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/observability"
	"kube-sre-mcp/internal/security"
)

type Server struct {
	Cfg     config.Config
	Factory *kube.Factory
	Actions *action.Engine
	Log     *observability.Metrics
	Logger  *slog.Logger
	cluster *kube.ClusterClient
	tools   []string
}

func New(cfg config.Config, cluster *kube.ClusterClient, factory *kube.Factory, metrics *observability.Metrics) *Server {
	s := &Server{
		Cfg:     cfg,
		Factory: factory,
		cluster: cluster,
		Log:     metrics,
		Actions: &action.Engine{Cfg: cfg, Cluster: cluster, Store: action.NewStore(cfg.ConfirmationTTL)},
	}
	s.Actions.ForContext = s.clusterFor
	return s
}

func (s *Server) clusterFor(ctxName string) (*kube.ClusterClient, error) {
	if s.cluster == nil {
		if s.Factory == nil {
			return nil, fmt.Errorf("no kubernetes cluster client")
		}
		return s.Factory.ForContext(ctxName)
	}
	if ctxName == "" || ctxName == s.cluster.ContextName {
		return s.cluster, nil
	}
	if s.Factory == nil {
		return nil, fmt.Errorf("context %q is not available", ctxName)
	}
	return s.Factory.ForContext(ctxName)
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, v, nil
}

func boolPtr(v bool) *bool { return &v }

func (s *Server) observe(tool, kubeCtx, kind, name, ns, status, health string, start time.Time) {
	d := time.Since(start)
	if s.Log != nil {
		s.Log.ObserveTool(tool, d)
	}
	if s.Logger != nil {
		s.Logger.Info("mcp_tool", observability.Attrs(fmt.Sprintf("%d", start.UnixNano()), tool, kubeCtx, kind, ns, d, health, status)...)
	}
}

func ro() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}
}

func writeAnn(destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPtr(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(true),
	}
}

type diagnoseArgs struct {
	Kind      string `json:"kind" jsonschema:"Resource kind or alias (po/pod, deploy, svc, ing, pvc, node, job, sts, ds, cronjob, etc.)"`
	Name      string `json:"name" jsonschema:"Resource name or prefix"`
	Namespace string `json:"namespace,omitempty" jsonschema:"Namespace (optional – auto-resolved if omitted)"`
	Context   string `json:"context,omitempty" jsonschema:"Optional kubeconfig context"`
}

type nameNSArgs struct {
	Name      string `json:"name" jsonschema:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema:"Namespace (optional)"`
	Context   string `json:"context,omitempty"`
}

type nodeArgs struct {
	Name    string `json:"name" jsonschema:"Node name"`
	Context string `json:"context,omitempty"`
}

type findArgs struct {
	Kind          string `json:"kind"`
	Query         string `json:"query"`
	Namespace     string `json:"namespace,omitempty"`
	AllNamespaces bool   `json:"all_namespaces,omitempty"`
	Context       string `json:"context,omitempty"`
}

type getArgs struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Context   string `json:"context,omitempty"`
}

type listArgs struct {
	Kind          string `json:"kind"`
	Namespace     string `json:"namespace,omitempty"`
	AllNamespaces bool   `json:"all_namespaces,omitempty"`
	LabelSelector string `json:"label_selector,omitempty"`
	FieldSelector string `json:"field_selector,omitempty"`
	Context       string `json:"context,omitempty"`
}

type logsArgs struct {
	Pod          string `json:"pod"`
	Namespace    string `json:"namespace,omitempty"`
	Container    string `json:"container,omitempty"`
	Previous     bool   `json:"previous,omitempty"`
	TailLines    int    `json:"tail_lines,omitempty"`
	SinceSeconds int64  `json:"since_seconds,omitempty"`
	Timestamps   *bool  `json:"timestamps,omitempty"`
	Context      string `json:"context,omitempty"`
}

type eventsArgs struct {
	Namespace     string `json:"namespace,omitempty"`
	ResourceName  string `json:"resource_name,omitempty"`
	ResourceKind  string `json:"resource_kind,omitempty"`
	AllNamespaces bool   `json:"all_namespaces,omitempty"`
	WarningsOnly  bool   `json:"warnings_only,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Context       string `json:"context,omitempty"`
}

type healthArgs struct {
	Namespace      string `json:"namespace,omitempty"`
	IncludeHealthy bool   `json:"include_healthy,omitempty"`
	MaxProblems    int    `json:"max_problems,omitempty"`
	Context        string `json:"context,omitempty"`
}

type emptyArgs struct {
	Context string `json:"context,omitempty"`
}

type restartArgs struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	Context        string `json:"context,omitempty" jsonschema:"Optional kubeconfig context; writes use this cluster"`
	ReasonCategory string `json:"reason_category,omitempty" jsonschema:"Optional RCA category; not trusted blindly"`
	Force          bool   `json:"force,omitempty" jsonschema:"Override restart_not_recommended"`
}

type scaleArgs struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Replicas       int32  `json:"replicas"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	Context        string `json:"context,omitempty"`
}

type deletePodArgs struct {
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	GracePeriodSeconds int64  `json:"grace_period_seconds,omitempty"`
	ConfirmationID     string `json:"confirmation_id,omitempty"`
	Context            string `json:"context,omitempty"`
}

type cordonArgs struct {
	Name           string `json:"name"`
	ConfirmationID string `json:"confirmation_id,omitempty"`
	Context        string `json:"context,omitempty"`
}

func (s *Server) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_diagnose_resource", Description: "PRIMARY DIAGNOSTIC TOOL. Diagnose why any Kubernetes resource is unhealthy. Resolves namespace, builds a resource graph, correlates status/events/logs/dependencies, and returns ranked root-cause hypotheses with evidence.", Annotations: ro()}, s.diagnoseResource)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_diagnose_pod", Description: "Deep-diagnose a Pod: container status, probes, scheduling, dependencies, events, logs, RCA.", Annotations: ro()}, s.diagnosePod)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_diagnose_deployment", Description: "Diagnose a Deployment including ReplicaSets and unhealthy Pods; aggregated workload RCA.", Annotations: ro()}, s.diagnoseDeploy)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_diagnose_service", Description: "Diagnose a Service: selector, EndpointSlices, ports, and selected Pod health.", Annotations: ro()}, s.diagnoseService)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_diagnose_node", Description: "Diagnose a Node: conditions, taints, cordon, events, optional metrics.", Annotations: ro()}, s.diagnoseNode)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_find_resource", Description: "Find a resource by name across namespaces. Prefix match is controlled; ambiguous matches are returned rather than guessed.", Annotations: ro()}, s.find)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_get_resource", Description: "Get a resource. Secrets return metadata and key names only — never .data.", Annotations: ro()}, s.get)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_list_resources", Description: "List resources of a kind. Honors label_selector and field_selector.", Annotations: ro()}, s.list)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_get_logs", Description: "Get Pod logs via the Kubernetes API. Auto-selects a failing container. Output is redacted.", Annotations: ro()}, s.logs)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_get_events", Description: "Get Kubernetes Events. Supports cluster-scoped Node events and all-namespaces. Failures are not swallowed.", Annotations: ro()}, s.events)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_cluster_health", Description: "Cluster health v2: lightweight inventory, unhealthy candidates, aggregated health for nodes/pods/workloads/storage.", Annotations: ro()}, s.clusterHealth)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_get_context", Description: "Get the current kubeconfig context, API server, and namespace.", Annotations: ro()}, s.getContext)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_list_contexts", Description: "List kubeconfig contexts.", Annotations: ro()}, s.listContexts)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_restart_deployment", Description: "Rollout-restart a Deployment via the Action Engine (SSAR, blast radius, confirmation).", Annotations: writeAnn(false, false)}, s.restart)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_scale_workload", Description: "Scale Deployment/StatefulSet/ReplicaSet via the Action Engine (HPA/max replica guards, confirmation).", Annotations: writeAnn(false, true)}, s.scale)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_delete_pod", Description: "Delete a Pod via the Action Engine. Unmanaged and control-plane pods are protected.", Annotations: writeAnn(true, false)}, s.deletePod)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_cordon_node", Description: "Cordon a Node. Refuses to cordon the last schedulable Ready node.", Annotations: writeAnn(false, true)}, s.cordon)
	mcp.AddTool(srv, &mcp.Tool{Name: "k8s_uncordon_node", Description: "Uncordon a Node via the Action Engine.", Annotations: writeAnn(false, true)}, s.uncordon)
	s.tools = []string{
		"k8s_diagnose_resource", "k8s_diagnose_pod", "k8s_diagnose_deployment", "k8s_diagnose_service", "k8s_diagnose_node",
		"k8s_find_resource", "k8s_get_resource", "k8s_list_resources", "k8s_get_logs", "k8s_get_events",
		"k8s_cluster_health", "k8s_get_context", "k8s_list_contexts",
		"k8s_restart_deployment", "k8s_scale_workload", "k8s_delete_pod", "k8s_cordon_node", "k8s_uncordon_node",
	}
}

func (s *Server) diagnoseResource(ctx context.Context, _ *mcp.CallToolRequest, args diagnoseArgs) (*mcp.CallToolResult, any, error) {
	return s.runDiag(ctx, args.Context, args.Kind, args.Name, args.Namespace, "k8s_diagnose_resource")
}

func (s *Server) diagnosePod(ctx context.Context, _ *mcp.CallToolRequest, args nameNSArgs) (*mcp.CallToolResult, any, error) {
	return s.runDiag(ctx, args.Context, "Pod", args.Name, args.Namespace, "k8s_diagnose_pod")
}

func (s *Server) diagnoseDeploy(ctx context.Context, _ *mcp.CallToolRequest, args nameNSArgs) (*mcp.CallToolResult, any, error) {
	return s.runDiag(ctx, args.Context, "Deployment", args.Name, args.Namespace, "k8s_diagnose_deployment")
}

func (s *Server) diagnoseService(ctx context.Context, _ *mcp.CallToolRequest, args nameNSArgs) (*mcp.CallToolResult, any, error) {
	return s.runDiag(ctx, args.Context, "Service", args.Name, args.Namespace, "k8s_diagnose_service")
}

func (s *Server) diagnoseNode(ctx context.Context, _ *mcp.CallToolRequest, args nodeArgs) (*mcp.CallToolResult, any, error) {
	return s.runDiag(ctx, args.Context, "Node", args.Name, "", "k8s_diagnose_node")
}

func (s *Server) runDiag(ctx context.Context, kubeCtx, kind, name, ns, tool string) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	cl, err := s.clusterFor(kubeCtx)
	if err != nil {
		s.observe(tool, kubeCtx, kind, name, ns, "error", "", start)
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	eng := diagnostic.Engine{Cfg: s.Cfg, Cluster: cl}
	out := eng.Diagnose(ctx, kind, name, ns)
	if s.Log != nil {
		s.Log.ObserveTool(tool, time.Since(start))
		s.Log.Diagnoses.WithLabelValues(string(out.Health)).Inc()
		if out.RootCause != nil {
			s.Log.RootCauses.WithLabelValues(out.RootCause.Category).Inc()
		}
		if out.Status == "error" || (out.Visibility != nil && out.Visibility.Limited) {
			s.Log.APIErrors.WithLabelValues(kind).Inc()
		}
	}
	if s.Logger != nil {
		rca := ""
		if out.RootCause != nil {
			rca = out.RootCause.Category
		}
		s.Logger.Info("mcp_tool", observability.Attrs(fmt.Sprintf("%d", start.UnixNano()), tool, cl.ContextName, kind, ns, time.Since(start), string(out.Health), rca)...)
	}
	return textResult(out)
}

func (s *Server) find(ctx context.Context, _ *mcp.CallToolRequest, args findArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	r := kube.NewResolver(cl, s.Cfg.PrefixMatch)
	res := r.ResolveFilter(ctx, args.Kind, args.Query, args.Namespace, args.AllNamespaces)
	if res.Status == kube.ResolveFound {
		obj, _ := r.Get(ctx, kube.NormalizeKind(args.Kind), res.Match.Name, res.Match.Namespace)
		return textResult(map[string]any{"found": true, "count": 1, "cluster": cl.Info(), "resources": []any{summarize(kube.NormalizeKind(args.Kind), obj, *res.Match)}})
	}
	if res.Status == kube.ResolveAmbiguous {
		return textResult(map[string]any{"found": true, "count": len(res.Matches), "cluster": cl.Info(), "resources": res.Matches})
	}
	if res.Status == kube.ResolveDenied {
		return textResult(map[string]any{"found": false, "status": "permission_denied", "message": res.Message})
	}
	return textResult(map[string]any{"found": false, "count": 0, "message": res.Message})
}

func (s *Server) get(ctx context.Context, _ *mcp.CallToolRequest, args getArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	kind := kube.NormalizeKind(args.Kind)
	r := kube.NewResolver(cl, s.Cfg.PrefixMatch)
	res := r.Resolve(ctx, kind, args.Name, args.Namespace)
	if res.Status != kube.ResolveFound {
		return textResult(map[string]any{"status": res.Status, "message": res.Message})
	}
	obj, err := r.Get(ctx, kind, res.Match.Name, res.Match.Namespace)
	if err != nil || obj == nil {
		return textResult(map[string]any{"status": "not_found"})
	}
	if kind == "Secret" {
		keys := []string{}
		if data, ok := obj.Object["data"].(map[string]any); ok {
			for k := range data {
				keys = append(keys, k)
			}
		}
		if sd, ok := obj.Object["stringData"].(map[string]any); ok {
			for k := range sd {
				keys = append(keys, k)
			}
		}
		typ, _, _ := unstructuredString(obj.Object, "type")
		return textResult(security.SecretSummary(obj.GetName(), obj.GetNamespace(), typ, keys, obj.GetCreationTimestamp().String()))
	}
	return textResult(summarize(kind, obj, *res.Match))
}

func unstructuredString(obj map[string]any, key string) (string, bool, error) {
	v, ok := obj[key].(string)
	return v, ok, nil
}

func (s *Server) list(ctx context.Context, _ *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	kind := kube.NormalizeKind(args.Kind)
	ns := args.Namespace
	if args.AllNamespaces {
		ns = ""
	} else if ns == "" && !kube.IsClusterScopedKind(kind) {
		ns = cl.Namespace
	}
	r := kube.NewResolver(cl, s.Cfg.PrefixMatch)
	items, err := r.List(ctx, kind, ns, args.LabelSelector, args.FieldSelector)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": security.Redact(err.Error())})
	}
	out := []any{}
	for i := range items {
		if i >= 200 {
			break
		}
		ref := model.ResourceRef{Kind: kind, Name: items[i].GetName(), Namespace: items[i].GetNamespace()}
		out = append(out, summarize(kind, &items[i], ref))
	}
	nslabel := ns
	if nslabel == "" {
		nslabel = "all"
	}
	return textResult(map[string]any{"kind": kind, "namespace": nslabel, "cluster": cl.Info(), "count": len(items), "items": out, "truncated": len(items) > 200})
}

func (s *Server) logs(ctx context.Context, _ *mcp.CallToolRequest, args logsArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	ns := args.Namespace
	if ns == "" {
		ns = cl.Namespace
	}
	container := args.Container
	r := kube.NewResolver(cl, s.Cfg.PrefixMatch)
	if container == "" {
		res := r.Resolve(ctx, "Pod", args.Pod, ns)
		if res.Status == kube.ResolveFound {
			obj, _ := r.Get(ctx, "Pod", res.Match.Name, res.Match.Namespace)
			if obj != nil {
				var p corev1.Pod
				_ = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &p)
				container = logs.SelectFailingContainer(&p)
				args.Pod = p.Name
				ns = p.Namespace
			} else if res.Match != nil {
				args.Pod = res.Match.Name
				ns = res.Match.Namespace
			}
		}
	}
	tail := int64(args.TailLines)
	if tail <= 0 {
		tail = 100
	}
	if tail > 500 {
		tail = 500
	}
	ts := true
	if args.Timestamps != nil {
		ts = *args.Timestamps
	}
	var since *int64
	if args.SinceSeconds > 0 {
		since = &args.SinceSeconds
	}
	ls := logs.Service{CS: cl.Clientset}
	out := ls.Get(ctx, args.Pod, ns, container, args.Previous, tail, since, ts)
	return textResult(out)
}

func (s *Server) events(ctx context.Context, _ *mcp.CallToolRequest, args eventsArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	ns := args.Namespace
	if ns == "" {
		ns = cl.Namespace
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	svc := events.Service{CS: cl.Clientset}
	list, err := svc.List(ctx, ns, args.ResourceName, args.ResourceKind, args.AllNamespaces, limit)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": security.Redact(err.Error())})
	}
	if args.WarningsOnly {
		filtered := list[:0]
		for _, e := range list {
			if e.Type == "Warning" {
				filtered = append(filtered, e)
			}
		}
		list = filtered
	}
	return textResult(map[string]any{"namespace": ns, "cluster": cl.Info(), "count": len(list), "events": list})
}

func (s *Server) clusterHealth(ctx context.Context, _ *mcp.CallToolRequest, args healthArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	max := args.MaxProblems
	if max <= 0 {
		max = 50
	}
	eng := diagnostic.Engine{Cfg: s.Cfg, Cluster: cl}
	rep := health.Cluster(ctx, cl, s.Cfg, args.Namespace, args.IncludeHealthy, max, eng.Diagnose)
	return textResult(rep)
}

func (s *Server) getContext(ctx context.Context, _ *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	info := cl.Info()
	return textResult(map[string]any{"status": "success", "context": info.Context, "server": info.Server, "namespace": info.Namespace})
}

func (s *Server) listContexts(ctx context.Context, _ *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
	cl, err := s.clusterFor(args.Context)
	if err != nil {
		return textResult(map[string]any{"status": "error", "message": err.Error()})
	}
	return textResult(map[string]any{"current": cl.ContextName, "contexts": cl.ListKubeContexts()})
}

func (s *Server) restart(ctx context.Context, _ *mcp.CallToolRequest, args restartArgs) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	out := s.Actions.RestartDeployment(ctx, args.Name, args.Namespace, args.ConfirmationID, args.ReasonCategory, args.Context, args.Force)
	s.trackAction("k8s_restart_deployment", "restart_deployment", args.Context, "Deployment", args.Name, args.Namespace, out.Status, start)
	return textResult(out)
}

func (s *Server) scale(ctx context.Context, _ *mcp.CallToolRequest, args scaleArgs) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	out := s.Actions.Scale(ctx, args.Kind, args.Name, args.Namespace, args.Replicas, args.ConfirmationID, args.Context)
	s.trackAction("k8s_scale_workload", "scale", args.Context, args.Kind, args.Name, args.Namespace, out.Status, start)
	return textResult(out)
}

func (s *Server) deletePod(ctx context.Context, _ *mcp.CallToolRequest, args deletePodArgs) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	g := args.GracePeriodSeconds
	if g == 0 {
		g = 30
	}
	out := s.Actions.DeletePod(ctx, args.Name, args.Namespace, g, args.ConfirmationID, args.Context)
	s.trackAction("k8s_delete_pod", "delete_pod", args.Context, "Pod", args.Name, args.Namespace, out.Status, start)
	return textResult(out)
}

func (s *Server) cordon(ctx context.Context, _ *mcp.CallToolRequest, args cordonArgs) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	out := s.Actions.Cordon(ctx, args.Name, args.ConfirmationID, args.Context, true)
	s.trackAction("k8s_cordon_node", "cordon", args.Context, "Node", args.Name, "", out.Status, start)
	return textResult(out)
}

func (s *Server) uncordon(ctx context.Context, _ *mcp.CallToolRequest, args cordonArgs) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	ctx, cancel := kube.WithTimeout(ctx, s.Cfg)
	defer cancel()
	out := s.Actions.Cordon(ctx, args.Name, args.ConfirmationID, args.Context, false)
	s.trackAction("k8s_uncordon_node", "uncordon", args.Context, "Node", args.Name, "", out.Status, start)
	return textResult(out)
}

func (s *Server) trackAction(tool, actionName, kubeCtx, kind, name, ns, status string, start time.Time) {
	s.observe(tool, kubeCtx, kind, name, ns, status, "", start)
	if s.Log != nil {
		s.Log.Actions.WithLabelValues(actionName, status).Inc()
		if status == "error" || status == "forbidden" || status == "preflight_failed" {
			s.Log.APIErrors.WithLabelValues(kind).Inc()
		}
	}
}
