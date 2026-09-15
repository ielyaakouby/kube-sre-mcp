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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

type Client struct {
	MC metricsclient.Interface
}

func (c *Client) Available() bool { return c != nil && c.MC != nil }

func (c *Client) Pod(ctx context.Context, ns, name string) (*metricsapi.PodMetrics, bool) {
	if !c.Available() {
		return nil, false
	}
	m, err := c.MC.MetricsV1beta1().PodMetricses(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, false
	}
	return m, true
}

func (c *Client) Node(ctx context.Context, name string) (*metricsapi.NodeMetrics, bool) {
	if !c.Available() {
		return nil, false
	}
	m, err := c.MC.MetricsV1beta1().NodeMetricses().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, false
	}
	return m, true
}
