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

package diagnostic

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func DiagnosePVC(cluster model.ClusterInfo, pvc *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume, sc *storagev1.StorageClass, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := model.ResourceRef{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: pvc.Namespace, Name: pvc.Name, UID: pvc.UID}
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-PVC-%d", id), signal.SourceStorage, "Storage", reason, sev, model.ConfidenceHigh, ref, msg))
		id++
	}
	scName := ""
	if pvc.Spec.StorageClassName != nil {
		scName = *pvc.Spec.StorageClassName
	}
	n("pvc_status", fmt.Sprintf("phase=%s storageClass=%s volume=%s", pvc.Status.Phase, scName, pvc.Spec.VolumeName), model.SeverityInfo)
	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		n("PVC_PENDING", "PVC is pending – not yet bound to a PersistentVolume.", model.SeverityCritical)
	case corev1.ClaimLost:
		n("PV_NOT_FOUND", "PVC is Lost – backing PV may have been deleted.", model.SeverityCritical)
	}
	if scName != "" && sc == nil {
		n("STORAGE_CLASS_NOT_FOUND", fmt.Sprintf("StorageClass %s not found", scName), model.SeverityCritical)
	}
	if pvc.Spec.VolumeName != "" && pv == nil {
		n("PV_NOT_FOUND", fmt.Sprintf("PV %s not found", pvc.Spec.VolumeName), model.SeverityWarning)
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	if pvc.Status.Phase == corev1.ClaimBound && resp.Health != model.HealthCritical {
		resp.Health = model.HealthHealthy
		resp.RootCause = nil
		resp.Summary = fmt.Sprintf("PVC %s is Bound.", pvc.Name)
	}
	return resp
}
