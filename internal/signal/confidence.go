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

func RankConfidence(a, b model.Confidence) int {
	return Score(a) - Score(b)
}

func Score(c model.Confidence) int {
	switch c {
	case model.ConfidenceConfirmed:
		return 4
	case model.ConfidenceHigh:
		return 3
	case model.ConfidenceMedium:
		return 2
	case model.ConfidenceLow:
		return 1
	default:
		return 0
	}
}

func MaxConfidence(cs ...model.Confidence) model.Confidence {
	best := model.ConfidenceLow
	for _, c := range cs {
		if Score(c) > Score(best) {
			best = c
		}
	}
	return best
}

func SourceQuality(s Source) int {
	switch s {
	case SourceStatus, SourceCondition:
		return 4
	case SourceEvent:
		return 3
	case SourceLog, SourceDependency:
		return 3
	case SourceScheduler, SourceStorage, SourceNetwork:
		return 2
	case SourceMetric:
		return 2
	default:
		return 1
	}
}
