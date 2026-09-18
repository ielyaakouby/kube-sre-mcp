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
	"k8s.io/apimachinery/pkg/types"

	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
)

type RelType string

const (
	Owns                RelType = "OWNS"
	OwnedBy             RelType = "OWNED_BY"
	Selects             RelType = "SELECTS"
	ScheduledOn         RelType = "SCHEDULED_ON"
	UsesConfigMap       RelType = "USES_CONFIGMAP"
	UsesSecret          RelType = "USES_SECRET"
	UsesPVC             RelType = "USES_PVC"
	UsesServiceAccount  RelType = "USES_SERVICE_ACCOUNT"
	UsesImagePullSecret RelType = "USES_IMAGE_PULL_SECRET" // #nosec G101 -- relation type name, not a credential
	BackedByPV          RelType = "BACKED_BY_PV"
	UsesStorageClass    RelType = "USES_STORAGE_CLASS"
	ExposedByService    RelType = "EXPOSED_BY_SERVICE"
	HasEndpoint         RelType = "HAS_ENDPOINT"
	RoutedByIngress     RelType = "ROUTED_BY_INGRESS"
	ScaledByHPA         RelType = "SCALED_BY_HPA"
	ProtectedByPDB      RelType = "PROTECTED_BY_PDB"
)

type Edge struct {
	Type RelType
	From model.ResourceRef
	To   model.ResourceRef
}

type Graph struct {
	Root             model.ResourceRef
	Nodes            []model.ResourceRef
	Edges            []Edge
	Truncated        bool
	TruncationReason string
	visited          map[types.UID]struct{}
	depth            map[string]int
}

func New(root model.ResourceRef) *Graph {
	g := &Graph{Root: root, visited: map[types.UID]struct{}{}, depth: map[string]int{}}
	g.AddNode(root)
	g.depth[keyRef(root)] = 0
	return g
}

func keyRef(n model.ResourceRef) string {
	return n.Kind + "/" + n.Namespace + "/" + n.Name
}

func (g *Graph) AddNode(n model.ResourceRef) bool {
	if n.UID != "" {
		if _, ok := g.visited[n.UID]; ok {
			return false
		}
		g.visited[n.UID] = struct{}{}
	} else {
		for _, e := range g.Nodes {
			if e.Kind == n.Kind && e.Name == n.Name && e.Namespace == n.Namespace {
				return false
			}
		}
	}
	g.Nodes = append(g.Nodes, n)
	return true
}

func (g *Graph) AddEdge(t RelType, from, to model.ResourceRef) {
	g.AddNode(from)
	g.AddNode(to)
	g.Edges = append(g.Edges, Edge{Type: t, From: from, To: to})
}

func (g *Graph) OfKind(kind string) []model.ResourceRef {
	var out []model.ResourceRef
	for _, n := range g.Nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

type Limits struct {
	MaxDepth int
	MaxNodes int
}
