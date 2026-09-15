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

package recommendation

import "kube-sre-mcp/internal/model"

func For(category string) []model.RecommendedAction {
	switch category {
	case "REGISTRY_AUTHENTICATION_FAILURE", "registry_authentication":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Verify imagePullSecret on the Pod/ServiceAccount (type kubernetes.io/dockerconfigjson)."},
			{Priority: 2, Action: "Inspect Secret metadata/keys only — never the .data payload."},
			{Priority: 3, Action: "Validate registry hostname and refresh expired credentials."},
		}
	case "IMAGE_NOT_FOUND", "image_not_found":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Verify image name and tag exist in the registry."},
		}
	case "OOM_KILLED", "oom_killed":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Compare current/previous memory usage with the container memory limit."},
			{Priority: 2, Action: "Inspect previous logs for allocation failures."},
			{Priority: 3, Action: "Raise the memory limit or fix a leak; restart alone will not fix a leak."},
		}
	case "CRASH_LOOP", "crash_loop":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Inspect current and previous container logs (already collected when possible)."},
			{Priority: 2, Action: "Check probes, env, ConfigMaps and Secrets."},
		}
	case "FAILED_MOUNT", "failed_mount", "FAILED_ATTACH_VOLUME":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Inspect PVC/Secret/ConfigMap volume dependencies and volume Events."},
		}
	case "INSUFFICIENT_CPU", "INSUFFICIENT_MEMORY":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Lower requests or add node capacity; inspect node allocatable vs requests."},
		}
	case "TAINT_NOT_TOLERATED":
		return []model.RecommendedAction{{Priority: 1, Action: "Add matching tolerations or remove the blocking taint."}}
	case "NODE_AFFINITY_MISMATCH", "NODE_SELECTOR_MISMATCH":
		return []model.RecommendedAction{{Priority: 1, Action: "Align nodeSelector/required nodeAffinity with node labels."}}
	case "READINESS_PROBE_FAILED", "LIVENESS_PROBE_FAILED", "STARTUP_PROBE_FAILED":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Fix the application endpoint or relax probe timing; do not restart blindly."},
		}
	case "SERVICE_NO_MATCHING_PODS", "SERVICE_NO_READY_PODS", "SERVICE_NO_ENDPOINTS":
		return []model.RecommendedAction{
			{Priority: 1, Action: "Diagnose selected Pods with k8s_diagnose_pod; verify labels and readiness."},
			{Priority: 2, Tool: "k8s_diagnose_pod", Action: "Deep-diagnose matching pods."},
		}
	case "INGRESS_BACKEND_MISSING", "INGRESS_TLS_SECRET_MISSING":
		return []model.RecommendedAction{{Priority: 1, Action: "Create the missing backend Service or TLS Secret."}}
	case "PVC_PENDING", "STORAGE_CLASS_NOT_FOUND", "PROVISIONING_FAILED":
		return []model.RecommendedAction{{Priority: 1, Action: "Inspect StorageClass, provisioner Events, and PV capacity/topology."}}
	case "PROGRESS_DEADLINE_EXCEEDED":
		return []model.RecommendedAction{{Priority: 1, Action: "Inspect ReplicaSet and Pod RCA; a restart will not fix image/config errors."}}
	default:
		return []model.RecommendedAction{{Priority: 1, Action: "Inspect supporting evidence and related graph nodes."}}
	}
}

func RestartUseful(category string) bool {
	switch category {
	case "REGISTRY_AUTHENTICATION_FAILURE", "IMAGE_NOT_FOUND", "IMAGE_PULL_FAILURE",
		"MISSING_SECRET", "MISSING_CONFIGMAP", "CONFIG_ERROR", "FAILED_MOUNT",
		"INSUFFICIENT_CPU", "INSUFFICIENT_MEMORY", "TAINT_NOT_TOLERATED",
		"UNSCHEDULABLE", "NO_ELIGIBLE_NODE", "FAILED_ATTACH_VOLUME":
		return false
	default:
		return true
	}
}
