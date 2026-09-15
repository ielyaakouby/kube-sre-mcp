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

package correlation

import (
	"sort"
	"strings"

	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/recommendation"
	"kube-sre-mcp/internal/signal"
)

func Correlate(signals []signal.DiagnosticSignal, impact *model.Impact) []model.RootCauseHypothesis {
	deduped := dedupEvents(signals)
	buckets := map[string][]signal.DiagnosticSignal{}
	for _, s := range deduped {
		cat := classify(s)
		if cat == "healthy" || cat == "noise" {
			continue
		}
		buckets[cat] = append(buckets[cat], s)
	}
	cats := make([]string, 0, len(buckets))
	for c := range buckets {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	var hyps []model.RootCauseHypothesis
	for _, cat := range cats {
		hyps = append(hyps, build(cat, buckets[cat], deduped, impact))
	}
	sort.SliceStable(hyps, func(i, j int) bool {
		if c := signal.RankConfidence(hyps[i].Confidence, hyps[j].Confidence); c != 0 {
			return c > 0
		}
		if si, sj := weightedScore(hyps[i]), weightedScore(hyps[j]); si != sj {
			return si > sj
		}
		if hyps[i].Category != hyps[j].Category {
			return hyps[i].Category < hyps[j].Category
		}
		return hyps[i].Title < hyps[j].Title
	})
	return hyps
}

func dedupEvents(in []signal.DiagnosticSignal) []signal.DiagnosticSignal {
	type key struct{ src, reason, msg, res string }
	seen := map[key]int{}
	order := make([]signal.DiagnosticSignal, 0, len(in))
	index := map[key]int{}
	for _, s := range in {
		if s.Source != signal.SourceEvent {
			order = append(order, s)
			continue
		}
		k := key{string(s.Source), s.Reason, s.Message, s.Resource.String()}
		if i, ok := index[k]; ok {
			order[i].Count += max(s.Count, 1)
			seen[k]++
			continue
		}
		if s.Count == 0 {
			s.Count = 1
		}
		index[k] = len(order)
		order = append(order, s)
		seen[k] = 1
	}
	return order
}

func weightedScore(h model.RootCauseHypothesis) int {
	bonus := 0
	switch h.Category {
	case "OOM_KILLED", "MISSING_SECRET", "MISSING_CONFIGMAP":
		bonus = 80
	case "REGISTRY_AUTHENTICATION_FAILURE":
		bonus = 70
	case "INSUFFICIENT_CPU", "INSUFFICIENT_MEMORY", "TAINT_NOT_TOLERATED", "NODE_SELECTOR_MISMATCH":
		bonus = 60
	case "SERVICE_NO_MATCHING_PODS", "PROGRESS_DEADLINE_EXCEEDED", "READINESS_PROBE_FAILED":
		bonus = 50
	case "CRASH_LOOP", "IMAGE_PULL_FAILURE":
		bonus = 20
	case "APPLICATION_AUTH_FAILURE":
		bonus = 15
	}
	return h.AffectedReplicas + signal.Score(h.Confidence)*10 + bonus
}

func classify(s signal.DiagnosticSignal) string {
	r := strings.ToLower(s.Reason + " " + s.Category + " " + s.Message)
	if isImagePullContext(s) && (strings.Contains(r, "unauthorized") || strings.Contains(r, "authentication") || strings.Contains(r, "denied")) {
		return "REGISTRY_AUTHENTICATION_FAILURE"
	}
	if strings.Contains(r, "manifest unknown") || (strings.Contains(r, "not found") && strings.Contains(r, "image")) {
		return "IMAGE_NOT_FOUND"
	}
	if strings.Contains(r, "rate limit") || strings.Contains(r, "toomanyrequests") {
		return "REGISTRY_RATE_LIMIT"
	}
	if strings.Contains(r, "x509") || (strings.Contains(r, "tls") && strings.Contains(r, "image")) {
		return "REGISTRY_TLS_ERROR"
	}
	if s.Source == signal.SourceLog && (strings.Contains(r, "auth_failure") || strings.Contains(r, "401") || strings.Contains(r, "403") ||
		strings.Contains(r, "unauthorized") || strings.Contains(r, "forbidden")) {
		return "APPLICATION_AUTH_FAILURE"
	}
	switch strings.ToUpper(s.Reason) {
	case "OOMKILLED", "OOM":
		return "OOM_KILLED"
	case "IMAGEPULLBACKOFF", "ERRIMAGEPULL":
		return "IMAGE_PULL_FAILURE"
	case "CRASHLOOPBACKOFF":
		return "CRASH_LOOP"
	case "READINESS_PROBE_FAILED":
		return "READINESS_PROBE_FAILED"
	case "LIVENESS_PROBE_FAILED":
		return "LIVENESS_PROBE_FAILED"
	case "STARTUP_PROBE_FAILED":
		return "STARTUP_PROBE_FAILED"
	case "INSUFFICIENT_CPU":
		return "INSUFFICIENT_CPU"
	case "INSUFFICIENT_MEMORY":
		return "INSUFFICIENT_MEMORY"
	case "NODE_SELECTOR_MISMATCH":
		return "NODE_SELECTOR_MISMATCH"
	case "NODE_AFFINITY_MISMATCH":
		return "NODE_AFFINITY_MISMATCH"
	case "TAINT_NOT_TOLERATED":
		return "TAINT_NOT_TOLERATED"
	case "NO_ELIGIBLE_NODE":
		return "NO_ELIGIBLE_NODE"
	case "UNSUPPORTED_SCHEDULER_CONSTRAINT":
		return "UNSUPPORTED_SCHEDULER_CONSTRAINT"
	}
	switch {
	case isImagePullContext(s) && (strings.Contains(r, "unauthorized") || strings.Contains(r, "authentication") || strings.Contains(r, "denied")):
		return "REGISTRY_AUTHENTICATION_FAILURE"
	case strings.Contains(r, "unauthorized") || strings.Contains(r, "authentication"):
		if isImagePullContext(s) {
			return "REGISTRY_AUTHENTICATION_FAILURE"
		}
		return "APPLICATION_AUTH_FAILURE"
	case strings.Contains(r, "manifest unknown") || (strings.Contains(r, "not found") && strings.Contains(r, "image")):
		return "IMAGE_NOT_FOUND"
	case strings.Contains(r, "rate limit") || strings.Contains(r, "toomanyrequests"):
		return "REGISTRY_RATE_LIMIT"
	case strings.Contains(r, "x509") || (strings.Contains(r, "tls") && strings.Contains(r, "image")):
		return "REGISTRY_TLS_ERROR"
	case strings.Contains(r, "imagepull") || strings.Contains(r, "errimagepull"):
		return "IMAGE_PULL_FAILURE"
	case strings.Contains(r, "oomkill") || s.Reason == "oom" || s.Reason == "OOMKilled":
		return "OOM_KILLED"
	case strings.Contains(r, "crashloop"):
		return "CRASH_LOOP"
	case strings.Contains(r, "missing_secret") || (strings.Contains(r, "secret") && strings.Contains(r, "createcontainerconfig")):
		return "MISSING_SECRET"
	case strings.Contains(r, "createcontainerconfig") || strings.Contains(r, "missing_configmap"):
		return "MISSING_CONFIGMAP"
	case strings.Contains(r, "createcontainer"):
		return "CONFIG_ERROR"
	case strings.Contains(r, "containercannotrun") || strings.Contains(r, "runcontainererror"):
		return "CONTAINER_CANNOT_RUN"
	case strings.Contains(r, "insufficient_cpu") || strings.Contains(r, "insufficient cpu"):
		return "INSUFFICIENT_CPU"
	case strings.Contains(r, "insufficient_memory") || strings.Contains(r, "insufficient memory"):
		return "INSUFFICIENT_MEMORY"
	case strings.Contains(r, "taint"):
		return "TAINT_NOT_TOLERATED"
	case strings.Contains(r, "nodeselector"):
		return "NODE_SELECTOR_MISMATCH"
	case strings.Contains(r, "affinity") || strings.Contains(r, "didn't match"):
		return "NODE_AFFINITY_MISMATCH"
	case strings.Contains(r, "unsupported_scheduler"):
		return "UNSUPPORTED_SCHEDULER_CONSTRAINT"
	case strings.Contains(r, "pvc") && strings.Contains(r, "schedul"):
		return "PVC_TOPOLOGY_CONFLICT"
	case strings.Contains(r, "failedscheduling") || strings.Contains(r, "unschedulable") || strings.Contains(r, "no_eligible"):
		return "NO_ELIGIBLE_NODE"
	case strings.Contains(r, "readiness"):
		return "READINESS_PROBE_FAILED"
	case strings.Contains(r, "liveness"):
		return "LIVENESS_PROBE_FAILED"
	case strings.Contains(r, "startup"):
		return "STARTUP_PROBE_FAILED"
	case strings.Contains(r, "failedattach"):
		return "FAILED_ATTACH_VOLUME"
	case strings.Contains(r, "failedmount") || s.Reason == "FailedMount":
		return "FAILED_MOUNT"
	case strings.Contains(r, "evict"):
		return "EVICTED"
	case strings.Contains(r, "progressdeadline"):
		return "PROGRESS_DEADLINE_EXCEEDED"
	case strings.Contains(r, "backoff_limit") || strings.Contains(r, "backofflimit"):
		return "BACKOFF_LIMIT_EXCEEDED"
	case strings.Contains(r, "job_pod_failed") || (s.Category == "Job" && strings.Contains(r, "failed")):
		return "JOB_POD_FAILED"
	case strings.Contains(r, "service_no_selector"):
		return "SERVICE_NO_SELECTOR"
	case strings.Contains(r, "service_no_matching") || strings.Contains(r, "nomatchingpods"):
		return "SERVICE_NO_MATCHING_PODS"
	case strings.Contains(r, "service_no_ready") || strings.Contains(r, "noreadypods"):
		return "SERVICE_NO_READY_PODS"
	case strings.Contains(r, "service_no_endpoints") || strings.Contains(r, "emptyendpoints"):
		return "SERVICE_NO_ENDPOINTS"
	case strings.Contains(r, "port_mismatch") || strings.Contains(r, "target_port"):
		return "SERVICE_PORT_MISMATCH"
	case strings.Contains(r, "ingress_backend_missing") || strings.Contains(r, "backendservicemissing"):
		return "INGRESS_BACKEND_MISSING"
	case strings.Contains(r, "ingress_backend_port"):
		return "INGRESS_BACKEND_PORT_INVALID"
	case strings.Contains(r, "ingress_no_ready"):
		return "INGRESS_NO_READY_ENDPOINTS"
	case strings.Contains(r, "tlssecret") || strings.Contains(r, "tls_secret"):
		return "INGRESS_TLS_SECRET_MISSING"
	case strings.Contains(r, "ingress_class"):
		return "INGRESS_CLASS_MISSING"
	case strings.Contains(r, "pvc_pending") || strings.Contains(r, "pvcpending"):
		return "PVC_PENDING"
	case strings.Contains(r, "storageclass") && strings.Contains(r, "not"):
		return "STORAGE_CLASS_NOT_FOUND"
	case strings.Contains(r, "provisioning"):
		return "PROVISIONING_FAILED"
	case strings.Contains(r, "diskpressure"):
		return "NODE_DISK_PRESSURE"
	case strings.Contains(r, "memorypressure"):
		return "NODE_MEMORY_PRESSURE"
	case strings.Contains(r, "pidpressure"):
		return "NODE_PID_PRESSURE"
	case strings.Contains(r, "networkunavailable"):
		return "NODE_NETWORK_UNAVAILABLE"
	case strings.Contains(r, "notready") && strings.Contains(r, "node"):
		return "NODE_NOT_READY"
	case s.Severity == model.SeverityCritical:
		return strings.ToUpper(s.Reason)
	case s.Severity == model.SeverityWarning:
		return strings.ToUpper(s.Reason)
	default:
		return "noise"
	}
}

func isImagePullContext(s signal.DiagnosticSignal) bool {
	blob := strings.ToLower(s.Reason + " " + s.Category + " " + s.Message)
	if strings.Contains(blob, "imagepull") || strings.Contains(blob, "errimagepull") ||
		strings.Contains(blob, "pulling image") || strings.Contains(blob, "failed to pull") ||
		strings.Contains(blob, "registry") || strings.Contains(blob, "imagepullbackoff") {
		return true
	}
	if s.Source == signal.SourceStatus || s.Source == signal.SourceEvent {
		switch strings.ToLower(s.Reason) {
		case "imagepullbackoff", "errimagepull", "failedtopullimage", "inspectfailed":
			return true
		}
	}
	return false
}

func build(cat string, ss []signal.DiagnosticSignal, all []signal.DiagnosticSignal, impact *model.Impact) model.RootCauseHypothesis {
	ids := make([]string, 0, len(ss))
	var msgs []string
	var affected []model.ResourceRef
	independent := map[signal.Source]int{}
	quality := 0
	authStatus := false
	onlyLogs := true
	for _, s := range ss {
		ids = append(ids, s.ID)
		independent[s.Source]++
		quality += signal.SourceQuality(s.Source) * max(s.Count, 1)
		if s.Message != "" && len(msgs) < 3 {
			msgs = append(msgs, s.Message)
		}
		affected = append(affected, s.Resource)
		if s.Source != signal.SourceLog {
			onlyLogs = false
		}
		if (s.Source == signal.SourceStatus || s.Source == signal.SourceCondition) && authoritative(cat, s) {
			authStatus = true
		}
	}
	conf := model.ConfidenceMedium
	if onlyLogs {
		conf = model.ConfidenceLow
	}
	if len(independent) >= 2 && independent[signal.SourceStatus]+independent[signal.SourceCondition] > 0 && independent[signal.SourceEvent] > 0 {
		conf = model.ConfidenceHigh
	}
	if authStatus {
		conf = model.ConfidenceConfirmed
	}
	if onlyLogs && conf == model.ConfidenceConfirmed {
		conf = model.ConfidenceLow
	}
	if !authStatus && conf == model.ConfidenceConfirmed {
		conf = model.ConfidenceHigh
	}
	title := strings.ReplaceAll(cat, "_", " ")
	h := model.RootCauseHypothesis{
		Category:                 cat,
		Title:                    title,
		Explanation:              strings.Join(msgs, " | "),
		Message:                  strings.Join(msgs, " | "),
		Confidence:               conf,
		AffectedResources:        uniqueRefs(affected),
		SupportingEvidenceIDs:    ids,
		ContradictingEvidenceIDs: contradictions(cat, all),
		Impact:                   impact,
		Recommendations:          recommendation.For(cat),
		AffectedReplicas:         len(uniqueRefs(affected)),
	}
	if len(h.ContradictingEvidenceIDs) > 0 {
		switch h.Confidence {
		case model.ConfidenceConfirmed:
			h.Confidence = model.ConfidenceHigh
		case model.ConfidenceHigh:
			h.Confidence = model.ConfidenceMedium
		}
	}
	_ = quality
	return h
}

func authoritative(cat string, s signal.DiagnosticSignal) bool {
	if s.Source != signal.SourceStatus && s.Source != signal.SourceCondition {
		return false
	}
	r := strings.ToLower(s.Reason + " " + s.Message)
	switch cat {
	case "OOM_KILLED":
		return strings.Contains(r, "oomkill")
	case "IMAGE_PULL_FAILURE", "REGISTRY_AUTHENTICATION_FAILURE", "IMAGE_NOT_FOUND":
		return strings.Contains(r, "imagepull") || strings.Contains(r, "errimagepull")
	case "PVC_PENDING":
		return strings.Contains(r, "pvc") || strings.Contains(r, "pending")
	case "NODE_NOT_READY":
		return strings.Contains(r, "ready") && strings.Contains(r, "false") || s.Reason == "Ready" || s.Reason == "NodeReady"
	case "CRASH_LOOP":
		return strings.Contains(r, "crashloop")
	case "READINESS_PROBE_FAILED", "LIVENESS_PROBE_FAILED", "STARTUP_PROBE_FAILED":
		return strings.Contains(strings.ToUpper(s.Reason), "PROBE")
	default:
		return false
	}
}

func contradictions(cat string, all []signal.DiagnosticSignal) []string {
	var out []string
	for _, s := range all {
		blob := strings.ToLower(s.Reason + " " + s.Message)
		switch cat {
		case "CRASH_LOOP":
			if s.Source == signal.SourceStatus && strings.Contains(blob, "appears healthy") {
				out = append(out, s.ID)
			}
			if s.Source == signal.SourceCondition && strings.Contains(blob, "ready=true") {
				out = append(out, s.ID)
			}
		case "READINESS_PROBE_FAILED":
			if strings.Contains(blob, "container") && strings.Contains(blob, "ready") && !strings.Contains(blob, "not ready") && s.Severity == model.SeverityInfo {
				out = append(out, s.ID)
			}
		}
	}
	sort.Strings(out)
	return uniqueStrings(out)
}

func uniqueRefs(in []model.ResourceRef) []model.ResourceRef {
	seen := map[string]struct{}{}
	var keys []string
	idx := map[string]model.ResourceRef{}
	for _, r := range in {
		k := r.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
		idx[k] = r
	}
	sort.Strings(keys)
	out := make([]model.ResourceRef, 0, len(keys))
	for _, k := range keys {
		out = append(out, idx[k])
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
