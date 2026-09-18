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
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/ielyaakouby/kube-sre-mcp/internal/graph"
	"github.com/ielyaakouby/kube-sre-mcp/internal/model"
	"github.com/ielyaakouby/kube-sre-mcp/internal/signal"
)

func DiagnoseJob(cluster model.ClusterInfo, j *batchv1.Job, pods []corev1.Pod, podSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := graph.JobRef(j)
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-JOB-%d", id), signal.SourceStatus, "Job", reason, sev, model.ConfidenceConfirmed, ref, msg))
		id++
	}
	n("job_status", fmt.Sprintf("active=%d succeeded=%d failed=%d backoffLimit=%v", j.Status.Active, j.Status.Succeeded, j.Status.Failed, j.Spec.BackoffLimit), model.SeverityInfo)
	failed := false
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			failed = true
			reason := c.Reason
			if reason == "BackoffLimitExceeded" {
				n("BACKOFF_LIMIT_EXCEEDED", c.Message, model.SeverityCritical)
			} else if reason == "DeadlineExceeded" {
				n("JOB_TIMEOUT", c.Message, model.SeverityCritical)
			} else {
				n("JOB_POD_FAILED", c.Message, model.SeverityCritical)
			}
		}
	}
	if j.Status.Failed > 0 && !failed {
		n("JOB_POD_FAILED", fmt.Sprintf("%d failed pod(s)", j.Status.Failed), model.SeverityCritical)
	}
	for _, p := range pods {
		sigs = append(sigs, podSignals[p.Name]...)
	}
	sigs = append(sigs, ev...)
	resp := assemble(cluster, ref, sigs, nil, "full")
	if j.Status.Succeeded > 0 && j.Status.Failed == 0 && !failed {
		resp.Health = model.HealthHealthy
		resp.RootCause = nil
		resp.Summary = fmt.Sprintf("Job %s succeeded.", j.Name)
	}
	return resp
}

func DiagnoseCronJob(cluster model.ClusterInfo, c *batchv1.CronJob, jobs []batchv1.Job, jobSignals map[string][]signal.DiagnosticSignal, ev []signal.DiagnosticSignal) model.DiagnosticResponse {
	ref := graph.CronRef(c)
	var sigs []signal.DiagnosticSignal
	id := 1
	n := func(reason, msg string, sev model.Severity) {
		sigs = append(sigs, signal.New(fmt.Sprintf("E-CJ-%d", id), signal.SourceStatus, "CronJob", reason, sev, model.ConfidenceHigh, ref, msg))
		id++
	}
	last := ""
	if c.Status.LastScheduleTime != nil {
		last = c.Status.LastScheduleTime.String()
	}
	n("cronjob_status", fmt.Sprintf("schedule=%s suspend=%v concurrencyPolicy=%s lastSchedule=%s lastSuccessful=%v active=%d",
		c.Spec.Schedule, c.Spec.Suspend != nil && *c.Spec.Suspend, c.Spec.ConcurrencyPolicy, last, c.Status.LastSuccessfulTime, len(c.Status.Active)), model.SeverityInfo)
	if c.Spec.ConcurrencyPolicy == batchv1.ForbidConcurrent && len(c.Status.Active) > 1 {
		n("concurrency_policy", "Forbid concurrencyPolicy with multiple active jobs", model.SeverityWarning)
	}
	if c.Spec.Suspend != nil && *c.Spec.Suspend {
		n("Suspended", "CronJob is suspended", model.SeverityWarning)
	}
	failedJobs := 0
	for i := range jobs {
		j := jobs[i]
		for _, cond := range j.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				failedJobs++
				n("JOB_POD_FAILED", fmt.Sprintf("Job %s failed: %s", j.Name, cond.Message), model.SeverityCritical)
			}
		}
		sigs = append(sigs, jobSignals[j.Name]...)
	}
	if failedJobs == 0 && (c.Spec.Suspend == nil || !*c.Spec.Suspend) && c.Status.LastSuccessfulTime == nil && c.Status.LastScheduleTime == nil {
		n("never_scheduled", "CronJob has never scheduled a Job", model.SeverityWarning)
	}
	sigs = append(sigs, ev...)
	return assemble(cluster, ref, sigs, nil, "full")
}
