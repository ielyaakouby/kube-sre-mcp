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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"kube-sre-mcp/internal/model"
)

type ResolveStatus string

const (
	ResolveFound     ResolveStatus = "found"
	ResolveAmbiguous ResolveStatus = "ambiguous"
	ResolveNotFound  ResolveStatus = "not_found"
	ResolveDenied    ResolveStatus = "permission_denied"
)

type ResolveResult struct {
	Status     ResolveStatus       `json:"status"`
	Match      *model.ResourceRef  `json:"match,omitempty"`
	Matches    []model.ResourceRef `json:"matches,omitempty"`
	Message    string              `json:"message,omitempty"`
	Resolution string              `json:"resolution,omitempty"`
}

type Resolver struct {
	cluster     *ClusterClient
	mapper      *MapperCache
	prefixMatch bool
}

func NewResolver(c *ClusterClient, prefixMatch bool) *Resolver {
	return &Resolver{
		cluster:     c,
		mapper:      NewMapperCache(c.Discovery, c.RESTMapper),
		prefixMatch: prefixMatch,
	}
}

func (r *Resolver) ContextNamespace() string {
	if r.cluster.Namespace != "" {
		return r.cluster.Namespace
	}
	return "default"
}

func (r *Resolver) GVR(kind string) (schema.GroupVersionResource, error) {
	return r.mapper.GVRForKind(kind)
}

func (r *Resolver) Get(ctx context.Context, kind, name, namespace string) (*unstructured.Unstructured, error) {
	kind = NormalizeKind(kind)
	gvr, err := r.mapper.GVRForKind(kind)
	if err != nil {
		return nil, err
	}
	clusterScoped := !r.mapper.IsNamespaced(kind)
	if clusterScoped {
		obj, err := r.cluster.Dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, Classify(err)
		}
		return obj, nil
	}
	if namespace == "" {
		return nil, &Error{Kind: ErrInvalid, Message: "namespace required"}
	}
	obj, err := r.cluster.Dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, Classify(err)
	}
	return obj, nil
}

func (r *Resolver) List(ctx context.Context, kind, namespace, labelSelector, fieldSelector string) ([]unstructured.Unstructured, error) {
	kind = NormalizeKind(kind)
	gvr, err := r.mapper.GVRForKind(kind)
	if err != nil {
		return nil, err
	}
	opts := metav1.ListOptions{LabelSelector: labelSelector, FieldSelector: fieldSelector}
	var list *unstructured.UnstructuredList
	if r.mapper.IsNamespaced(kind) == false || namespace == "" {
		list, err = r.cluster.Dynamic.Resource(gvr).List(ctx, opts)
	} else {
		list, err = r.cluster.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, opts)
	}
	if err != nil {
		return nil, Classify(err)
	}
	return list.Items, nil
}

func (r *Resolver) listNamespaces(ctx context.Context) ([]string, error) {
	ns, err := r.cluster.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		if IsForbidden(err) {
			return []string{r.ContextNamespace(), "default"}, Classify(err)
		}
		return []string{r.ContextNamespace(), "default"}, err
	}
	out := make([]string, 0, len(ns.Items))
	for _, n := range ns.Items {
		out = append(out, n.Name)
	}
	return unique(out), nil
}

func (r *Resolver) Resolve(ctx context.Context, kind, name, namespace string) ResolveResult {
	return r.ResolveFilter(ctx, kind, name, namespace, false)
}

func (r *Resolver) ResolveFilter(ctx context.Context, kind, name, namespace string, allNamespaces bool) ResolveResult {
	kind = NormalizeKind(kind)
	if name == "" {
		return ResolveResult{Status: ResolveNotFound, Message: "name is required"}
	}

	if !r.mapper.IsNamespaced(kind) {
		obj, err := r.Get(ctx, kind, name, "")
		if err == nil && obj != nil {
			return ResolveResult{Status: ResolveFound, Match: refFrom(obj, kind), Resolution: "exact"}
		}
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
		items, lerr := r.List(ctx, kind, "", "", "")
		if IsForbidden(lerr) {
			return ResolveResult{Status: ResolveDenied, Message: lerr.Error()}
		}
		return buildResult(kind, name, prefixFilter(kind, items, name, r.prefixMatch))
	}

	if namespace != "" && !allNamespaces {
		obj, err := r.Get(ctx, kind, name, namespace)
		if err == nil && obj != nil {
			return ResolveResult{Status: ResolveFound, Match: refFrom(obj, kind), Resolution: "exact"}
		}
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
		if IsNotFound(err) || err != nil {
			if r.prefixMatch {
				items, lerr := r.List(ctx, kind, namespace, "", "")
				if IsForbidden(lerr) {
					return ResolveResult{Status: ResolveDenied, Message: lerr.Error()}
				}
				return buildResult(kind, name, prefixFilter(kind, items, name, true))
			}
			return ResolveResult{Status: ResolveNotFound, Message: fmt.Sprintf("No %s named %q in namespace %s.", kind, name, namespace)}
		}
	}

	if allNamespaces {
		items, err := r.List(ctx, kind, "", "", "")
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
		if err != nil {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
		return buildResult(kind, name, prefixFilter(kind, items, name, r.prefixMatch))
	}

	priority := unique(nonEmpty(namespace, r.ContextNamespace(), "default"))
	for _, ns := range priority {
		obj, err := r.Get(ctx, kind, name, ns)
		if err == nil && obj != nil {
			return ResolveResult{Status: ResolveFound, Match: refFrom(obj, kind), Resolution: "exact"}
		}
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
	}

	allNs, nsErr := r.listNamespaces(ctx)
	if IsForbidden(nsErr) {
		return ResolveResult{Status: ResolveDenied, Message: fmt.Sprintf("cannot list namespaces: %v", nsErr)}
	}
	exact := []model.ResourceRef{}
	for _, ns := range allNs {
		if contains(priority, ns) {
			continue
		}
		obj, err := r.Get(ctx, kind, name, ns)
		if err == nil && obj != nil {
			exact = append(exact, *refFrom(obj, kind))
		}
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
	}
	if len(exact) == 1 {
		return ResolveResult{Status: ResolveFound, Match: &exact[0], Resolution: "exact"}
	}
	if len(exact) > 1 {
		return ResolveResult{Status: ResolveAmbiguous, Matches: exact, Message: "multiple exact matches; specify namespace"}
	}

	if !r.prefixMatch {
		return ResolveResult{Status: ResolveNotFound, Message: fmt.Sprintf("No %s named %q found in any accessible namespace.", kind, name)}
	}
	prefixHits := []model.ResourceRef{}
	for _, ns := range allNs {
		items, err := r.List(ctx, kind, ns, "", "")
		if IsForbidden(err) {
			return ResolveResult{Status: ResolveDenied, Message: err.Error()}
		}
		if err != nil {
			continue
		}
		prefixHits = append(prefixHits, prefixFilter(kind, items, name, true)...)
	}
	return buildResult(kind, name, prefixHits)
}

func refFrom(obj *unstructured.Unstructured, kind string) *model.ResourceRef {
	return &model.ResourceRef{
		APIVersion: obj.GetAPIVersion(),
		Kind:       kind,
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
	}
}

func prefixFilter(kind string, items []unstructured.Unstructured, name string, prefix bool) []model.ResourceRef {
	var exact, pref []model.ResourceRef
	for i := range items {
		n := items[i].GetName()
		if n == name {
			exact = append(exact, *refFrom(&items[i], kind))
			continue
		}
		if prefix && len(name) >= 3 && len(n) >= len(name) && n[:len(name)] == name {
			pref = append(pref, *refFrom(&items[i], kind))
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return pref
}

func buildResult(kind, query string, matches []model.ResourceRef) ResolveResult {
	if len(matches) == 0 {
		return ResolveResult{Status: ResolveNotFound, Message: fmt.Sprintf("No %s matching query.", kind)}
	}
	if len(matches) == 1 {
		res := ResolveResult{Status: ResolveFound, Match: &matches[0], Resolution: "exact"}
		if query != "" && matches[0].Name != query {
			res.Resolution = "prefix"
		}
		return res
	}
	return ResolveResult{Status: ResolveAmbiguous, Matches: matches, Message: "ambiguous matches; specify namespace or exact name"}
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func nonEmpty(vals ...string) []string { return unique(vals) }

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
