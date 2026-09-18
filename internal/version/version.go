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

package version

import "strings"

// Version is the release identifier used by the binary, MCP implementation
// metadata, and Kubernetes HTTP User-Agent. Override at build time with:
//
//	go build -ldflags "-X github.com/ielyaakouby/kube-sre-mcp/internal/version.Version=0.1.0"
//
// Git tags may include a leading "v"; String() always reports a v-prefixed
// value (for example v0.1.0). UserAgent() uses the unprefixed form.
var Version = "0.1.0"

func numeric() string {
	return strings.TrimPrefix(Version, "v")
}

func String() string {
	v := numeric()
	if v == "" {
		return "v0.0.0-dev"
	}
	return "v" + v
}

// UserAgent is the Kubernetes client User-Agent, for example kube-sre-mcp/0.1.0.
func UserAgent() string {
	return "kube-sre-mcp/" + numeric()
}
