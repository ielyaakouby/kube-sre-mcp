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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/security"
)

func summarize(kind string, obj *unstructured.Unstructured, ref model.ResourceRef) any {
	if obj == nil {
		return ref
	}
	base := map[string]any{
		"kind": kind, "name": obj.GetName(), "namespace": obj.GetNamespace(),
		"creationTimestamp": obj.GetCreationTimestamp(),
		"labels":            obj.GetLabels(),
		"annotations":       security.RedactMap(obj.GetAnnotations()),
	}
	st, _, _ := unstructured.NestedMap(obj.Object, "status")
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	switch kind {
	case "Pod":
		base["phase"], _, _ = unstructured.NestedString(st, "phase")
		base["node"], _, _ = unstructured.NestedString(spec, "nodeName")
		return base
	case "Deployment":
		base["desired"], _, _ = unstructured.NestedInt64(spec, "replicas")
		base["ready"], _, _ = unstructured.NestedInt64(st, "readyReplicas")
		base["available"], _, _ = unstructured.NestedInt64(st, "availableReplicas")
		base["unavailable"], _, _ = unstructured.NestedInt64(st, "unavailableReplicas")
		return base
	case "Service":
		base["type"], _, _ = unstructured.NestedString(spec, "type")
		base["clusterIP"], _, _ = unstructured.NestedString(spec, "clusterIP")
		base["selector"], _, _ = unstructured.NestedStringMap(spec, "selector")
		return base
	case "Node":
		base["unschedulable"], _, _ = unstructured.NestedBool(spec, "unschedulable")
		return base
	case "PersistentVolumeClaim":
		base["phase"], _, _ = unstructured.NestedString(st, "phase")
		return base
	case "Secret":
		keys := []string{}
		if data, ok := obj.Object["data"].(map[string]any); ok {
			for k := range data {
				keys = append(keys, k)
			}
		}
		typ, _ := obj.Object["type"].(string)
		return security.SecretSummary(obj.GetName(), obj.GetNamespace(), typ, keys, obj.GetCreationTimestamp().String())
	case "ConfigMap":
		if data, ok := obj.Object["data"].(map[string]any); ok {
			keys := []string{}
			for k := range data {
				keys = append(keys, k)
			}
			base["keys"] = keys
		}
		return base
	default:
		if safe := summarizeUnknownStatus(st); len(safe) > 0 {
			base["status"] = safe
		}
		return base
	}
}

const (
	maxUnknownConditions = 8
	maxConditionMessage  = 240
)

func summarizeUnknownStatus(st map[string]any) map[string]any {
	if st == nil {
		return nil
	}
	out := map[string]any{}
	if phase, ok, _ := unstructured.NestedString(st, "phase"); ok && phase != "" {
		out["phase"] = security.Redact(phase)
	}
	for _, k := range []string{"replicas", "readyReplicas", "availableReplicas", "unavailableReplicas", "updatedReplicas", "currentReplicas", "observedGeneration"} {
		if v, ok, _ := unstructured.NestedInt64(st, k); ok {
			out[k] = v
		}
	}
	conds, _, _ := unstructured.NestedSlice(st, "conditions")
	if summarized := summarizeConditions(conds); len(summarized) > 0 {
		out["conditions"] = summarized
		if len(conds) > maxUnknownConditions {
			out["conditions_truncated"] = true
		}
	}
	return out
}

func summarizeConditions(conds []any) []map[string]any {
	if len(conds) == 0 {
		return nil
	}
	limit := len(conds)
	if limit > maxUnknownConditions {
		limit = maxUnknownConditions
	}
	out := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		m, ok := conds[i].(map[string]any)
		if !ok {
			continue
		}
		item := map[string]any{}
		for _, k := range []string{"type", "status", "reason", "message"} {
			v, exists := m[k]
			if !exists {
				continue
			}
			s, ok := v.(string)
			if !ok {
				continue
			}
			s = security.Redact(s)
			if k == "message" && len(s) > maxConditionMessage {
				s = s[:maxConditionMessage] + "…"
			}
			item[k] = s
		}
		if len(item) > 0 {
			out = append(out, item)
		}
	}
	return out
}
