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

package logs

import (
	"context"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ielyaakouby/kube-sre-mcp/internal/security"
)

type Result struct {
	Pod       string   `json:"pod"`
	Namespace string   `json:"namespace"`
	Container string   `json:"container"`
	Previous  bool     `json:"previous"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
	Error     string   `json:"error,omitempty"`
}

type Service struct {
	CS kubernetes.Interface
}

func (s *Service) Get(ctx context.Context, pod, ns, container string, previous bool, tail int64, since *int64, timestamps bool) Result {
	base := Result{Pod: pod, Namespace: ns, Container: container, Previous: previous}
	opts := &corev1.PodLogOptions{
		Container:  container,
		Previous:   previous,
		Timestamps: timestamps,
	}
	if tail > 0 {
		opts.TailLines = &tail
	}
	if since != nil {
		opts.SinceSeconds = since
	}
	req := s.CS.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		base.Error = security.Redact(err.Error())
		return base
	}
	defer stream.Close()
	raw, err := io.ReadAll(stream)
	if err != nil {
		base.Error = security.Redact(err.Error())
		return base
	}
	lines := splitLines(string(raw))
	for i := range lines {
		lines[i] = security.Redact(lines[i])
	}
	if int64(len(lines)) > tail && tail > 0 {
		lines = lines[len(lines)-int(tail):]
		base.Truncated = true
	}
	base.Lines = lines
	return base
}

func splitLines(s string) []string {
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func SelectFailingContainer(pod *corev1.Pod) string {
	all := append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...)
	all = append(all, pod.Status.ContainerStatuses...)
	for _, cs := range all {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing" {
			return cs.Name
		}
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			return cs.Name
		}
	}
	if len(pod.Spec.Containers) > 0 {
		return pod.Spec.Containers[0].Name
	}
	return ""
}
