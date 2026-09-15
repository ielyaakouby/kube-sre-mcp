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
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
)

var aliases = map[string]string{
	"po": "Pod", "pod": "Pod", "pods": "Pod",
	"deploy": "Deployment", "deployment": "Deployment", "deployments": "Deployment",
	"sts": "StatefulSet", "statefulset": "StatefulSet", "statefulsets": "StatefulSet",
	"ds": "DaemonSet", "daemonset": "DaemonSet", "daemonsets": "DaemonSet",
	"svc": "Service", "service": "Service", "services": "Service",
	"ing": "Ingress", "ingress": "Ingress", "ingresses": "Ingress",
	"cm": "ConfigMap", "configmap": "ConfigMap", "configmaps": "ConfigMap",
	"secret": "Secret", "secrets": "Secret",
	"pvc": "PersistentVolumeClaim", "persistentvolumeclaim": "PersistentVolumeClaim",
	"pv": "PersistentVolume", "persistentvolume": "PersistentVolume",
	"ns": "Namespace", "namespace": "Namespace", "namespaces": "Namespace",
	"sa": "ServiceAccount", "serviceaccount": "ServiceAccount",
	"job": "Job", "jobs": "Job",
	"cronjob": "CronJob", "cronjobs": "CronJob",
	"rs": "ReplicaSet", "replicaset": "ReplicaSet", "replicasets": "ReplicaSet",
	"hpa":  "HorizontalPodAutoscaler",
	"pdb":  "PodDisruptionBudget",
	"node": "Node", "nodes": "Node",
	"ep": "Endpoints", "endpoints": "Endpoints",
	"eps": "EndpointSlice", "endpointslice": "EndpointSlice", "endpointslices": "EndpointSlice",
	"netpol": "NetworkPolicy", "networkpolicy": "NetworkPolicy",
	"role": "Role", "clusterrole": "ClusterRole",
	"rolebinding": "RoleBinding", "clusterrolebinding": "ClusterRoleBinding",
	"quota": "ResourceQuota", "resourcequota": "ResourceQuota",
	"sc": "StorageClass", "storageclass": "StorageClass",
}

func NormalizeKind(raw string) string {
	if raw == "" {
		return raw
	}
	if v, ok := aliases[strings.ToLower(raw)]; ok {
		return v
	}
	return raw
}

var knownGVR = map[string]schema.GroupVersionResource{
	"Pod":                     {Group: "", Version: "v1", Resource: "pods"},
	"Deployment":              {Group: "apps", Version: "v1", Resource: "deployments"},
	"ReplicaSet":              {Group: "apps", Version: "v1", Resource: "replicasets"},
	"StatefulSet":             {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"DaemonSet":               {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"Service":                 {Group: "", Version: "v1", Resource: "services"},
	"Endpoints":               {Group: "", Version: "v1", Resource: "endpoints"},
	"EndpointSlice":           {Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"},
	"ConfigMap":               {Group: "", Version: "v1", Resource: "configmaps"},
	"Secret":                  {Group: "", Version: "v1", Resource: "secrets"},
	"PersistentVolumeClaim":   {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	"ServiceAccount":          {Group: "", Version: "v1", Resource: "serviceaccounts"},
	"Ingress":                 {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	"NetworkPolicy":           {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	"Job":                     {Group: "batch", Version: "v1", Resource: "jobs"},
	"CronJob":                 {Group: "batch", Version: "v1", Resource: "cronjobs"},
	"HorizontalPodAutoscaler": {Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"},
	"PodDisruptionBudget":     {Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"},
	"Role":                    {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"},
	"RoleBinding":             {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"},
	"ResourceQuota":           {Group: "", Version: "v1", Resource: "resourcequotas"},
	"LimitRange":              {Group: "", Version: "v1", Resource: "limitranges"},
	"Node":                    {Group: "", Version: "v1", Resource: "nodes"},
	"Namespace":               {Group: "", Version: "v1", Resource: "namespaces"},
	"PersistentVolume":        {Group: "", Version: "v1", Resource: "persistentvolumes"},
	"StorageClass":            {Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"},
	"ClusterRole":             {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"},
	"ClusterRoleBinding":      {Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"},
	"IngressClass":            {Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"},
}

func KnownGVR(kind string) (schema.GroupVersionResource, bool) {
	gvr, ok := knownGVR[NormalizeKind(kind)]
	return gvr, ok
}

func IsClusterScopedKind(kind string) bool {
	switch NormalizeKind(kind) {
	case "Node", "Namespace", "PersistentVolume", "StorageClass", "ClusterRole", "ClusterRoleBinding", "IngressClass":
		return true
	default:
		return false
	}
}

type MapperCache struct {
	mu     sync.Mutex
	mapper meta.RESTMapper
	disco  discovery.DiscoveryInterface
}

func NewMapperCache(disco discovery.DiscoveryInterface, mapper meta.RESTMapper) *MapperCache {
	return &MapperCache{disco: disco, mapper: mapper}
}

func (m *MapperCache) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mapper = nil
}

func (m *MapperCache) ensureLocked() error {
	if m.mapper == nil && m.disco != nil {
		gr, err := restmapper.GetAPIGroupResources(m.disco)
		if err != nil {
			return err
		}
		m.mapper = restmapper.NewDiscoveryRESTMapper(gr)
	}
	if m.mapper == nil {
		return fmt.Errorf("no RESTMapper available")
	}
	return nil
}

func (m *MapperCache) mappingLocked(kind string) (*meta.RESTMapping, error) {
	if err := m.ensureLocked(); err != nil {
		return nil, err
	}
	gvk := schema.GroupVersionKind{Kind: kind}
	mapping, err := m.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		mappings, err2 := m.mapper.RESTMappings(schema.GroupKind{Kind: kind})
		if err2 == nil && len(mappings) > 0 {
			return mappings[0], nil
		}
		if mp := m.scanDiscovery(kind); mp != nil {
			return mp, nil
		}
		if err2 != nil {
			return nil, err2
		}
		return nil, err
	}
	return mapping, nil
}

func (m *MapperCache) scanDiscovery(kind string) *meta.RESTMapping {
	if m.disco == nil {
		return nil
	}
	_, resources, err := m.disco.ServerGroupsAndResources()
	if err != nil || resources == nil {
		return nil
	}
	for _, list := range resources {
		gv, perr := schema.ParseGroupVersion(list.GroupVersion)
		if perr != nil {
			continue
		}
		for _, ar := range list.APIResources {
			if ar.Kind != kind {
				continue
			}
			scope := meta.RESTScopeNamespace
			if !ar.Namespaced {
				scope = meta.RESTScopeRoot
			}
			return &meta.RESTMapping{
				Resource:         gv.WithResource(ar.Name),
				GroupVersionKind: gv.WithKind(kind),
				Scope:            scope,
			}
		}
	}
	return nil
}

func (m *MapperCache) GVRForKind(kind string) (schema.GroupVersionResource, error) {
	kind = NormalizeKind(kind)
	if gvr, ok := knownGVR[kind]; ok {
		return gvr, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mapping, err := m.mappingLocked(kind)
	if meta.IsNoMatchError(err) && m.disco != nil {
		m.mapper = nil
		if mapping, err = m.mappingLocked(kind); err != nil {
			return schema.GroupVersionResource{}, err
		}
	}
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return mapping.Resource, nil
}

func (m *MapperCache) MappingForKind(kind string) (*meta.RESTMapping, error) {
	kind = NormalizeKind(kind)
	if gvr, ok := knownGVR[kind]; ok {
		scope := meta.RESTScopeNamespace
		if IsClusterScopedKind(kind) {
			scope = meta.RESTScopeRoot
		}
		return &meta.RESTMapping{
			Resource: gvr,
			GroupVersionKind: schema.GroupVersionKind{
				Group: gvr.Group, Version: gvr.Version, Kind: kind,
			},
			Scope: scope,
		}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mapping, err := m.mappingLocked(kind)
	if meta.IsNoMatchError(err) && m.disco != nil {
		m.mapper = nil
		mapping, err = m.mappingLocked(kind)
	}
	return mapping, err
}

func (m *MapperCache) IsNamespaced(kind string) bool {
	mp, err := m.MappingForKind(kind)
	if err != nil || mp == nil {
		return !IsClusterScopedKind(kind)
	}
	return mp.Scope.Name() == meta.RESTScopeNameNamespace
}
