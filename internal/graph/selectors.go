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

package graph

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
)

func MatchLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func MatchLabelSelector(sel *metav1.LabelSelector, labels map[string]string) bool {
	if sel == nil {
		return false
	}
	parsed, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return false
	}
	if parsed.Empty() {
		return false
	}
	return parsed.Matches(k8slabels.Set(labels))
}

func SelectorString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	s := ""
	i := 0
	for k, v := range m {
		if i > 0 {
			s += ","
		}
		s += k + "=" + v
		i++
	}
	return s
}

func PodMatchesService(p *corev1.Pod, selector map[string]string) bool {
	return MatchLabels(selector, p.Labels)
}
