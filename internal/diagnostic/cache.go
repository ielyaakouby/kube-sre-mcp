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
	"context"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// apiCache is request-scoped. It never outlives a single Diagnose call.
type apiCache struct {
	mu sync.Mutex
	m  map[string]cacheEnt
}

type cacheEnt struct {
	obj any
	err error
}

func newAPICache() *apiCache {
	return &apiCache{m: map[string]cacheEnt{}}
}

func cacheKey(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "|"
		}
		out += p
	}
	return out
}

func (c *apiCache) get(ctx context.Context, key string, load func() (any, error)) (any, error) {
	if c == nil {
		return load()
	}
	c.mu.Lock()
	if e, ok := c.m[key]; ok {
		c.mu.Unlock()
		return e.obj, e.err
	}
	c.mu.Unlock()
	obj, err := load()
	if ctx.Err() != nil || apierrors.IsTimeout(err) {
		return obj, err
	}
	c.mu.Lock()
	c.m[key] = cacheEnt{obj: obj, err: err}
	c.mu.Unlock()
	return obj, err
}
