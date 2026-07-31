package k8s_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

const (
	diagNS      = "default"
	diagRelease = "pv-migrate-abc12-src"
)

func diagLabels() map[string]string {
	return map[string]string{"app.kubernetes.io/instance": diagRelease}
}

func warningEvent(reason, message string, involved *corev1.ObjectReference, count int32) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: diagNS, Name: reason + "-" + involved.Name},
		Type:           corev1.EventTypeWarning,
		Reason:         reason,
		Message:        message,
		Count:          count,
		InvolvedObject: *involved,
	}
}

func writeDiagnostics(ctx context.Context, t *testing.T, objects ...runtime.Object) string {
	t.Helper()

	var buf bytes.Buffer

	k8s.WriteWorkloadDiagnostics(ctx, fake.NewClientset(objects...), diagNS,
		k8s.InstanceLabelSelector(diagRelease), console.Palette{}, &buf, slog.New(slog.DiscardHandler))

	return buf.String()
}

func TestWriteWorkloadDiagnostics_PendingPodWithSchedulingEvent(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-sshd-abc", Labels: diagLabels(), UID: types.UID("pod-uid"),
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}

	out := writeDiagnostics(t.Context(), t, pod,
		warningEvent("FailedScheduling", "0/1 nodes are available: 1 node(s) didn't match node selector.",
			&corev1.ObjectReference{Kind: "Pod", Name: pod.Name, UID: pod.UID}, 3),
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Namespace: diagNS, Name: "normal"},
			Type:           corev1.EventTypeNormal,
			Reason:         "Scheduled",
			Message:        "nothing to see here",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: pod.Name, UID: pod.UID},
		})

	assert.Contains(t, out, "pod "+pod.Name+": Pending")
	assert.Contains(t, out, "warning FailedScheduling: 0/1 nodes are available")
	assert.Contains(t, out, "(x3)", "the API's own repeat count is used instead of hand-rolled dedup")
	// The fake clientset ignores field selectors, so what this exercises is the
	// in-code warning-type guard, not the server-side selector.
	assert.NotContains(t, out, "nothing to see here", "only warnings are reported")
}

func TestWriteWorkloadDiagnostics_ImagePullBackOff(t *testing.T) {
	t.Parallel()

	out := writeDiagnostics(t.Context(), t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-rsync-abc", Labels: diagLabels(), UID: types.UID("pod-uid"),
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "rsync",
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ImagePullBackOff",
						Message: `Back-off pulling image "docker.io/library/nosuchimage:latest"`,
					}},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"},
					},
				},
			},
		},
	})

	assert.Contains(t, out, "container rsync waiting: ImagePullBackOff: Back-off pulling image")
	assert.Contains(t, out, "container rsync previously terminated: exit code 1")
	assert.NotContains(t, out, "reason: Error",
		"the generic reason Kubernetes attaches to every non-zero exit says nothing")
}

// TestWriteWorkloadDiagnostics_ZeroPodsWithFailedCreate is the admission class:
// PodSecurity, ResourceQuota and missing service accounts reject the pod before
// it exists, so the only record is an event on the workload that tried.
func TestWriteWorkloadDiagnostics_ZeroPodsWithFailedCreate(t *testing.T) {
	t.Parallel()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-rsync", Labels: diagLabels(), UID: types.UID("job-uid"),
		},
	}

	out := writeDiagnostics(t.Context(), t, job,
		warningEvent("FailedCreate", `pods "x" is forbidden: violates PodSecurity "restricted:latest"`,
			&corev1.ObjectReference{Kind: "Job", Name: job.Name, UID: job.UID}, 1))

	assert.Contains(t, out, "job "+job.Name+":")
	assert.Contains(t, out, `warning FailedCreate: pods "x" is forbidden: violates PodSecurity`)
	assert.NotContains(t, out, "(x", "a single occurrence carries no repeat count")
}

// TestWriteWorkloadDiagnostics_EventMatchedByUIDNotName pins the identity rule.
// The chart's sshd Deployment and Service share a name, so a name-keyed event
// would be attributed to the wrong object.
func TestWriteWorkloadDiagnostics_EventMatchedByUIDNotName(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-sshd", Labels: diagLabels(), UID: types.UID("service-uid"),
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
	}

	out := writeDiagnostics(t.Context(), t, service,
		warningEvent("FailedCreate", "this belongs to the deployment of the same name",
			&corev1.ObjectReference{Kind: "Deployment", Name: service.Name, UID: types.UID("deployment-uid")}, 1))

	assert.Contains(t, out, "service "+service.Name+": LoadBalancer")
	assert.Contains(t, out, "no external address assigned")
	assert.NotContains(t, out, "this belongs to the deployment of the same name")
}

func TestWriteWorkloadDiagnostics_LoadBalancerWithAddress(t *testing.T) {
	t.Parallel()

	out := writeDiagnostics(t.Context(), t, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-sshd", Labels: diagLabels(), UID: types.UID("service-uid"),
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}}},
		},
	})

	assert.Contains(t, out, "external address: 10.0.0.1")
}

func TestWriteWorkloadDiagnostics_ReportsOwnerWorkloads(t *testing.T) {
	t.Parallel()

	out := writeDiagnostics(t.Context(), t,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: diagNS, Name: diagRelease + "-sshd", Labels: diagLabels(), UID: types.UID("deployment-uid"),
			},
			Status: appsv1.DeploymentStatus{Replicas: 1},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: diagNS,
				Name:      diagRelease + "-sshd-1",
				Labels:    diagLabels(),
				UID:       types.UID("replicaset-uid"),
			},
			Status: appsv1.ReplicaSetStatus{Replicas: 1},
		})

	assert.Contains(t, out, "deployment "+diagRelease+"-sshd: 0/1 ready")
	assert.Contains(t, out, "replicaset "+diagRelease+"-sshd-1: 0/1 ready")
}

// TestWriteWorkloadDiagnostics_CancelledParentContext is a smoke test only: the
// fake clientset never reads the context, so it cannot prove the detachment.
// The property itself is pinned by the internal test on the detached context.
func TestWriteWorkloadDiagnostics_CancelledParentContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	out := writeDiagnostics(ctx, t, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: diagNS, Name: diagRelease + "-rsync-abc", Labels: diagLabels(), UID: types.UID("pod-uid"),
		},
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	})

	require.Contains(t, out, "pod "+diagRelease+"-rsync-abc: Failed")
}

func TestWriteWorkloadDiagnostics_NoResources(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "    no resources found\n", writeDiagnostics(t.Context(), t))
}

// TestWriteWorkloadDiagnostics_UnreadableCluster separates "nothing exists" from
// "could not look": every list refused must not be reported as absence.
func TestWriteWorkloadDiagnostics_UnreadableCluster(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()
	cli.PrependReactor("list", "*", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})

	var buf bytes.Buffer

	k8s.WriteWorkloadDiagnostics(t.Context(), cli, diagNS,
		k8s.InstanceLabelSelector(diagRelease), console.Palette{}, &buf, slog.New(slog.DiscardHandler))

	assert.Equal(t, "    could not read the cluster's resources\n", buf.String())
}
