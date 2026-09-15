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

package metrics

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func HighMemoryVsLimit(pod *corev1.Pod, pm *metricsapi.PodMetrics) bool {
	if pod == nil || pm == nil {
		return false
	}
	lim := map[string]resource.Quantity{}
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			lim[c.Name] = q
		}
	}
	for _, c := range pm.Containers {
		limit, ok := lim[c.Name]
		if !ok || limit.IsZero() {
			continue
		}
		used := c.Usage[corev1.ResourceMemory]
		thr := limit.DeepCopy()
		// 90% of limit
		thr.Set(limit.Value() * 9 / 10)
		if used.Cmp(thr) >= 0 {
			return true
		}
	}
	return false
}
