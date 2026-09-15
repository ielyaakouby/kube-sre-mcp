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

package events

import "strings"

func Classify(reason, message string) (category, severity, explanation, hint string) {
	r := reason
	m := strings.ToLower(message)
	switch r {
	case "FailedScheduling", "Preempted", "Evicted":
		return "Scheduling", "critical", "The scheduler could not place the pod.", "Inspect node resources, taints, affinity, and PVC topology."
	case "ErrImagePull", "ImagePullBackOff":
		return "Image", "critical", "The kubelet could not pull the container image.", "Verify image name, registry auth, and imagePullSecrets."
	case "BackOff":
		lm := strings.ToLower(reason + " " + message)
		if strings.Contains(lm, "image") || strings.Contains(lm, "pull") {
			return "Image", "critical", "The kubelet is backing off an image pull.", "Verify image name, registry auth, and imagePullSecrets."
		}
		return "Container", "warning", "The kubelet is backing off restarting a failed container.", "Inspect container lastState and logs."
	case "Killing", "Failed", "Created", "Started":
		sev := "info"
		if r == "Failed" || r == "Killing" {
			sev = "warning"
		}
		return "Container", sev, "Container lifecycle event.", "Inspect container lastState and logs."
	case "Unhealthy":
		return "Probe", "warning", "A probe reported the container unhealthy.", "Inspect readiness/liveness/startup probes."
	case "FailedMount", "FailedAttachVolume", "FailedBinding", "ProvisioningFailed", "VolumeResizeFailed":
		return "Storage", "critical", "Volume attach/mount/provision failed.", "Inspect PVC/PV/StorageClass and volume events."
	case "FailedCreatePodSandBox", "NetworkNotReady":
		return "Networking", "critical", "Pod network sandbox or CNI is not ready.", "Inspect node NetworkUnavailable and CNI."
	case "FailedCreate", "ReplicaFailure", "ProgressDeadlineExceeded":
		return "Workload", "critical", "Workload controller could not create or progress replicas.", "Inspect child ReplicaSets/Pods."
	case "FailedGetResourceMetric", "FailedGetScale":
		return "Autoscaling", "warning", "HPA could not read metrics or scale target.", "Inspect metrics.k8s.io and scale target."
	}
	if strings.Contains(m, "unauthorized") || strings.Contains(m, "authentication") {
		return "Image", "critical", "Registry authentication failure.", "Refresh imagePullSecret credentials."
	}
	if r != "" {
		sev := "info"
		return "Other", sev, "Event reason " + r, ""
	}
	return "Other", "info", "", ""
}
