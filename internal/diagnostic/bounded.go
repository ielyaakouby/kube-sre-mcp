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
)

func (e *Engine) runBounded(ctx context.Context, n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if n == 1 {
		fn(0)
		return
	}
	max := e.Cfg.MaxConcurrentK8s
	if max <= 0 {
		max = 8
	}
	sem := make(chan struct{}, max)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}
			fn(i)
		}()
	}
	wg.Wait()
}
