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

package signal

import "kube-sre-mcp/internal/model"

func MaxSeverity(ss ...model.Severity) model.Severity {
	best := model.SeverityInfo
	for _, s := range ss {
		if rank(s) > rank(best) {
			best = s
		}
	}
	return best
}

func rank(s model.Severity) int {
	switch s {
	case model.SeverityCritical:
		return 3
	case model.SeverityWarning:
		return 2
	default:
		return 1
	}
}

func HealthFromSeverity(s model.Severity, hasCritical bool, unknown bool) model.Health {
	if unknown {
		return model.HealthUnknown
	}
	if hasCritical || s == model.SeverityCritical {
		return model.HealthCritical
	}
	if s == model.SeverityWarning {
		return model.HealthDegraded
	}
	return model.HealthHealthy
}
