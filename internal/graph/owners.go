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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

func OwnerRefs(obj metav1.Object, ns string) []model.ResourceRef {
	var out []model.ResourceRef
	for _, o := range obj.GetOwnerReferences() {
		out = append(out, model.ResourceRef{
			APIVersion: o.APIVersion,
			Kind:       o.Kind,
			Namespace:  ns,
			Name:       o.Name,
			UID:        o.UID,
		})
	}
	return out
}

func PodRef(p *corev1.Pod) model.ResourceRef {
	return model.ResourceRef{APIVersion: "v1", Kind: "Pod", Namespace: p.Namespace, Name: p.Name, UID: p.UID}
}

func DeployRef(d *appsv1.Deployment) model.ResourceRef {
	return model.ResourceRef{APIVersion: "apps/v1", Kind: "Deployment", Namespace: d.Namespace, Name: d.Name, UID: d.UID}
}

func STSRef(s *appsv1.StatefulSet) model.ResourceRef {
	return model.ResourceRef{APIVersion: "apps/v1", Kind: "StatefulSet", Namespace: s.Namespace, Name: s.Name, UID: s.UID}
}

func DSRef(d *appsv1.DaemonSet) model.ResourceRef {
	return model.ResourceRef{APIVersion: "apps/v1", Kind: "DaemonSet", Namespace: d.Namespace, Name: d.Name, UID: d.UID}
}

func JobRef(j *batchv1.Job) model.ResourceRef {
	return model.ResourceRef{APIVersion: "batch/v1", Kind: "Job", Namespace: j.Namespace, Name: j.Name, UID: j.UID}
}

func CronRef(c *batchv1.CronJob) model.ResourceRef {
	return model.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: c.Namespace, Name: c.Name, UID: c.UID}
}

func OwnedByUID(obj metav1.Object, uid types.UID) bool {
	for _, o := range obj.GetOwnerReferences() {
		if o.UID == uid {
			return true
		}
	}
	return false
}

func STSOwnsPVC(s *appsv1.StatefulSet, pvc *corev1.PersistentVolumeClaim) bool {
	if OwnedByUID(pvc, s.UID) {
		return true
	}
	for _, vct := range s.Spec.VolumeClaimTemplates {
		prefix := vct.Name + "-" + s.Name + "-"
		if len(pvc.Name) <= len(prefix) || pvc.Name[:len(prefix)] != prefix {
			continue
		}
		ord := pvc.Name[len(prefix):]
		if ord == "" {
			continue
		}
		ok := true
		for _, c := range ord {
			if c < '0' || c > '9' {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func ReplicaSetRevision(rs *appsv1.ReplicaSet) int {
	v := rs.Annotations["deployment.kubernetes.io/revision"]
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func CurrentReplicaSets(d *appsv1.Deployment, rss []appsv1.ReplicaSet) (current []appsv1.ReplicaSet, old []appsv1.ReplicaSet) {
	var owned []appsv1.ReplicaSet
	maxRev := -1
	for i := range rss {
		if !OwnedByUID(&rss[i], d.UID) {
			continue
		}
		owned = append(owned, rss[i])
		if r := ReplicaSetRevision(&rss[i]); r > maxRev {
			maxRev = r
		}
	}
	if maxRev < 0 {
		return owned, nil
	}
	for i := range owned {
		if ReplicaSetRevision(&owned[i]) == maxRev {
			current = append(current, owned[i])
		} else {
			old = append(old, owned[i])
		}
	}
	return current, old
}
