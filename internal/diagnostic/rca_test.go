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

package diagnostic_test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"kube-sre-mcp/internal/diagnostic"
	"kube-sre-mcp/internal/logs"
	"kube-sre-mcp/internal/model"
	"kube-sre-mcp/internal/signal"
)

func cluster() model.ClusterInfo { return model.ClusterInfo{Context: "test", Namespace: "default"} }

func pod(name string, phase corev1.PodPhase, waiting string, msg string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "reg.example/app:1.0"}}},
		Status: corev1.PodStatus{Phase: phase, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app",
		}}},
	}
	if waiting != "" {
		p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: waiting, Message: msg}
		p.Status.ContainerStatuses[0].Ready = false
	} else if phase == corev1.PodRunning {
		p.Status.ContainerStatuses[0].Ready = true
		p.Status.ContainerStatuses[0].State.Running = &corev1.ContainerStateRunning{}
	}
	return p
}

func TestPodHealthy(t *testing.T) {
	p := pod("ok", corev1.PodRunning, "", "")
	r := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p})
	if r.Health != model.HealthHealthy {
		t.Fatalf("health=%s rca=%v", r.Health, r.RootCause)
	}
}

func TestPodImagePullVariants(t *testing.T) {
	cases := []struct {
		msg string
		cat string
	}{
		{"unauthorized: authentication required", "REGISTRY_AUTHENTICATION_FAILURE"},
		{"manifest unknown", "IMAGE_NOT_FOUND"},
		{"x509: certificate signed by unknown authority", "REGISTRY_TLS_ERROR"},
		{"toomanyrequests: You have reached your pull rate limit", "REGISTRY_RATE_LIMIT"},
		{"dial tcp timeout", "IMAGE_PULL_FAILURE"},
	}
	for _, tc := range cases {
		p := pod("p", corev1.PodPending, "ImagePullBackOff", tc.msg)
		ev := []signal.DiagnosticSignal{signal.New("e1", signal.SourceEvent, "Image", "ErrImagePull", model.SeverityCritical, model.ConfidenceHigh, model.ResourceRef{Kind: "Pod", Name: "p"}, tc.msg)}
		r := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p, Events: ev, SecretsExist: map[string]bool{"regcred": true}})
		if r.RootCause == nil {
			t.Fatalf("msg %q: no rca health=%s", tc.msg, r.Health)
		}
		if r.RootCause.Category != tc.cat {
			t.Fatalf("msg %q: got %s want %s", tc.msg, r.RootCause.Category, tc.cat)
		}
		if r.Health != model.HealthCritical {
			t.Fatalf("expected critical")
		}
	}
}

func TestPodCrashOOMProbesScheduleMount(t *testing.T) {
	p := pod("c", corev1.PodRunning, "CrashLoopBackOff", "")
	p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}
	r := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p})
	if r.RootCause == nil || r.RootCause.Category != "OOM_KILLED" {
		t.Fatalf("got %#v", r.RootCause)
	}

	p2 := pod("c2", corev1.PodRunning, "CrashLoopBackOff", "exit 1")
	p2.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1}
	r2 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p2, LogSignals: logs.Analyze(model.ResourceRef{Kind: "Pod", Name: "c2"}, "app", []string{"panic: boom"})})
	if r2.RootCause == nil || r2.RootCause.Category != "CRASH_LOOP" {
		t.Fatalf("crash got %#v", r2.RootCause)
	}

	p3 := pod("cfg", corev1.PodPending, "CreateContainerConfigError", "secret missing")
	r3 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p3, SecretsExist: map[string]bool{}})
	if r3.Health != model.HealthCritical {
		t.Fatal(r3.Health)
	}

	p4 := pod("run", corev1.PodPending, "ContainerCannotRun", "exec format error")
	r4 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: p4})
	if r4.RootCause == nil {
		t.Fatal("expected rca")
	}

	pend := pod("pend", corev1.PodPending, "", "")
	pend.Status.ContainerStatuses = nil
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1", Labels: map[string]string{"disk": "ssd"}}}
	n.Status.Allocatable = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}
	pend.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("8Gi")}
	r5 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pend, Node: n})
	if r5.RootCause == nil || (r5.RootCause.Category != "INSUFFICIENT_CPU" && r5.RootCause.Category != "INSUFFICIENT_MEMORY" && r5.RootCause.Category != "NO_ELIGIBLE_NODE") {
		t.Fatalf("sched %#v findings=%v", r5.RootCause, r5.Findings)
	}

	nt := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2"}, Spec: corev1.NodeSpec{Taints: []corev1.Taint{{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule}}}}
	nt.Status.Allocatable = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("32Gi")}
	pt := pod("taint", corev1.PodPending, "", "")
	pt.Status.ContainerStatuses = nil
	r6 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pt, Node: nt})
	if r6.RootCause == nil || r6.RootCause.Category != "TAINT_NOT_TOLERATED" {
		t.Fatalf("taint %#v", r6.RootCause)
	}

	na := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n3", Labels: map[string]string{"zone": "a"}}}
	na.Status.Allocatable = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("32Gi")}
	pa := pod("aff", corev1.PodPending, "", "")
	pa.Status.ContainerStatuses = nil
	pa.Spec.NodeSelector = map[string]string{"zone": "b"}
	r7 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pa, Node: na})
	if r7.RootCause == nil || r7.RootCause.Category != "NODE_SELECTOR_MISMATCH" {
		t.Fatalf("selector %#v", r7.RootCause)
	}

	pm := pod("m", corev1.PodPending, "FailedMount", "mount timeout")
	r8 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pm, Events: []signal.DiagnosticSignal{signal.New("e", signal.SourceEvent, "Storage", "FailedMount", model.SeverityCritical, model.ConfidenceHigh, model.ResourceRef{Kind: "Pod", Name: "m"}, "FailedMount")}})
	if r8.RootCause == nil || r8.RootCause.Category != "FAILED_MOUNT" {
		t.Fatalf("mount %#v", r8.RootCause)
	}

	pa2 := pod("att", corev1.PodPending, "FailedAttachVolume", "attach")
	r9 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pa2})
	if r9.RootCause == nil {
		t.Fatal("attach")
	}

	pr := pod("ready", corev1.PodRunning, "", "")
	pr.Status.ContainerStatuses[0].Ready = false
	pr.Spec.Containers[0].ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/h", Port: intstr.FromInt(8080)}}}
	r10 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: pr})
	if r10.RootCause == nil || r10.RootCause.Category != "READINESS_PROBE_FAILED" {
		t.Fatalf("probe %#v health=%s findings=%v", r10.RootCause, r10.Health, r10.Findings)
	}

	miss := pod("miss", corev1.PodPending, "CreateContainerConfigError", "configmap")
	miss.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "X", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"}, Key: "k"}}}}
	r11 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: miss, ConfigMaps: map[string]*corev1.ConfigMap{}})
	if r11.RootCause == nil || r11.RootCause.Category != "MISSING_CONFIGMAP" {
		t.Fatalf("cm %#v", r11.RootCause)
	}

	misss := pod("secs", corev1.PodPending, "CreateContainerConfigError", "secret")
	misss.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "X", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}, Key: "k"}}}}
	r12 := diagnostic.DiagnosePod(cluster(), diagnostic.PodInput{Pod: misss, SecretsExist: map[string]bool{"s": false}})
	if r12.RootCause == nil || r12.RootCause.Category != "MISSING_SECRET" {
		t.Fatalf("sec %#v", r12.RootCause)
	}
}

func i32(v int32) *int32 { return &v }

func TestWorkloads(t *testing.T) {
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: i32(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 3, AvailableReplicas: 3}}
	r := diagnostic.DiagnoseDeployment(cluster(), d, nil, nil, nil)
	if r.Health != model.HealthHealthy {
		t.Fatal(r.Health)
	}
	d2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: i32(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UnavailableReplicas: 2}}
	p := pod("api-1", corev1.PodPending, "ImagePullBackOff", "unauthorized")
	ps := map[string][]signal.DiagnosticSignal{"api-1": {signal.New("e", signal.SourceStatus, "Image", "ImagePullBackOff", model.SeverityCritical, model.ConfidenceConfirmed, model.ResourceRef{Kind: "Pod", Name: "api-1"}, "unauthorized")}}
	r2 := diagnostic.DiagnoseDeployment(cluster(), d2, []corev1.Pod{*p}, ps, nil)
	if r2.Health == model.HealthHealthy || r2.RootCause == nil {
		t.Fatalf("%s %#v", r2.Health, r2.RootCause)
	}
	if r2.Impact == nil || r2.Impact.UnavailableReplicas != 2 {
		t.Fatalf("impact %#v", r2.Impact)
	}

	d3 := d2.DeepCopy()
	d3.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded", Message: "timed out"}}
	r3 := diagnostic.DiagnoseDeployment(cluster(), d3, nil, nil, nil)
	if r3.RootCause == nil || r3.RootCause.Category != "PROGRESS_DEADLINE_EXCEEDED" {
		t.Fatalf("pde %#v findings=%v", r3.RootCause, r3.Findings)
	}

	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "ns"}, Spec: appsv1.StatefulSetSpec{Replicas: i32(2)}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 2}}
	if diagnostic.DiagnoseStatefulSet(cluster(), sts, nil, nil, nil, nil).Health != model.HealthHealthy {
		t.Fatal("sts healthy")
	}
	sts2 := sts.DeepCopy()
	sts2.Status.ReadyReplicas = 0
	pvc := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "db-0"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	r4 := diagnostic.DiagnoseStatefulSet(cluster(), sts2, nil, []corev1.PersistentVolumeClaim{pvc}, nil, nil)
	if r4.Health == model.HealthHealthy {
		t.Fatal(r4)
	}

	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds"}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 3, NumberReady: 3}}
	if diagnostic.DiagnoseDaemonSet(cluster(), ds, nil, nil, nil).Health != model.HealthHealthy {
		t.Fatal("ds")
	}
	ds2 := ds.DeepCopy()
	ds2.Status.NumberReady = 1
	if diagnostic.DiagnoseDaemonSet(cluster(), ds2, nil, nil, nil).Health == model.HealthHealthy {
		t.Fatal("ds unhealthy")
	}

	j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "j"}, Status: batchv1.JobStatus{Succeeded: 1}}
	if diagnostic.DiagnoseJob(cluster(), j, nil, nil, nil).Health != model.HealthHealthy {
		t.Fatal("job ok")
	}
	j2 := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "j"}, Spec: batchv1.JobSpec{BackoffLimit: i32(1)}, Status: batchv1.JobStatus{Failed: 2, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}}}}
	rj := diagnostic.DiagnoseJob(cluster(), j2, nil, nil, nil)
	if rj.RootCause == nil || rj.RootCause.Category != "BACKOFF_LIMIT_EXCEEDED" {
		t.Fatalf("job %#v", rj.RootCause)
	}

	suspend := true
	cj := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cj"}, Spec: batchv1.CronJobSpec{Schedule: "* * * * *"}}
	if diagnostic.DiagnoseCronJob(cluster(), cj, nil, nil, nil).Health == model.HealthCritical {
		t.Fatal("cron")
	}
	cj2 := cj.DeepCopy()
	cj2.Spec.Suspend = &suspend
	if diagnostic.DiagnoseCronJob(cluster(), cj2, nil, nil, nil).Health != model.HealthDegraded {
		t.Fatal("suspend")
	}
	failed := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "cj-1"}, Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "fail"}}}}
	rcj := diagnostic.DiagnoseCronJob(cluster(), cj, []batchv1.Job{failed}, nil, nil)
	if rcj.Health == model.HealthHealthy {
		t.Fatal("cron failed job should not be healthy")
	}
}

func TestServiceIngressPVCNode(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "x"}, Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(8080)}}}}
	r := diagnostic.DiagnoseService(cluster(), svc, nil, nil, nil, nil, nil, true)
	if r.RootCause == nil || r.RootCause.Category != "SERVICE_NO_MATCHING_PODS" {
		t.Fatalf("%#v", r.RootCause)
	}
	svc2 := svc.DeepCopy()
	svc2.Spec.Selector = nil
	r2 := diagnostic.DiagnoseService(cluster(), svc2, nil, nil, nil, nil, nil, true)
	if r2.RootCause == nil || r2.RootCause.Category != "SERVICE_NO_SELECTOR" {
		t.Fatalf("selector %#v", r2.RootCause)
	}

	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "i", Namespace: "ns"}, Spec: networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{{IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "missing", Port: networkingv1.ServiceBackendPort{Number: 80}}}}}}}}},
		TLS:   []networkingv1.IngressTLS{{SecretName: "tls"}},
	}}
	ri := diagnostic.DiagnoseIngress(cluster(), ing, map[string]*corev1.Service{}, nil, map[string]bool{"tls": false}, nil, nil)
	if ri.RootCause == nil {
		t.Fatal("ingress")
	}

	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: strPtr("sc")}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	rp := diagnostic.DiagnosePVC(cluster(), pvc, nil, nil, nil)
	if rp.Health == model.HealthHealthy {
		t.Fatal("pvc")
	}
	sc := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "sc"}}
	bound := pvc.DeepCopy()
	bound.Status.Phase = corev1.ClaimBound
	if diagnostic.DiagnosePVC(cluster(), bound, nil, sc, nil).Health != model.HealthHealthy {
		t.Fatal("bound")
	}

	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	if diagnostic.DiagnoseNode(cluster(), n, 2, nil, false).Health != model.HealthHealthy {
		t.Fatal("node")
	}
	n2 := n.DeepCopy()
	n2.Status.Conditions[0].Status = corev1.ConditionFalse
	if diagnostic.DiagnoseNode(cluster(), n2, 2, nil, false).Health != model.HealthCritical {
		t.Fatal("notready")
	}
	n3 := n.DeepCopy()
	n3.Status.Conditions = append(n3.Status.Conditions, corev1.NodeCondition{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue})
	if diagnostic.DiagnoseNode(cluster(), n3, 0, nil, false).Health != model.HealthCritical {
		t.Fatal("disk")
	}
	n4 := n.DeepCopy()
	n4.Spec.Unschedulable = true
	if diagnostic.DiagnoseNode(cluster(), n4, 0, nil, false).Health != model.HealthDegraded {
		t.Fatal("cordon")
	}
}

func strPtr(s string) *string { return &s }

func TestRBACUnknown(t *testing.T) {
	vis := &model.Visibility{Limited: true, MissingPermissions: []model.Permission{{Verb: "get", Resource: "pods"}}}
	if vis.Limited != true {
		t.Fatal("visibility")
	}
}
