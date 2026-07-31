package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

// failedPodError explains a failed job pod with what the cluster reported and
// nothing else: the container's terminated state when it is there, the pod's own
// reason otherwise. The exit code is joined by the data mover's documented
// meaning for it, which is a statement about the documentation rather than a
// claim about what happened.
func failedPodError(
	ctx context.Context,
	cli kubernetes.Interface,
	jobName string,
	pod *corev1.Pod,
) error {
	terminated := terminatedContainerState(pod, jobName)

	// A watch event can carry the failed phase one step ahead of the container
	// status, so refetch once before giving up on the exit code.
	if terminated == nil {
		if fresh, err := cli.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{}); err == nil {
			pod = fresh
			terminated = terminatedContainerState(pod, jobName)
		}
	}

	// "pod", not "job": the name carries the generated suffix, and calling it a
	// job sends whoever copies it into kubectl after an object that is not there.
	if terminated == nil {
		return fmt.Errorf("pod %s/%s failed%s", pod.Namespace, pod.Name, podStatusDetail(pod))
	}

	return fmt.Errorf("pod %s/%s failed: %s", pod.Namespace, pod.Name, describeTerminated(jobName, terminated))
}

// describeTerminated renders everything the terminated state carries. An
// OOMKilled reason next to code 137 explains more than any table row does, so
// nothing here replaces anything else. The one exception is the reason string
// "Error", which Kubernetes attaches to any non-zero exit and which therefore
// says nothing.
func describeTerminated(jobName string, terminated *corev1.ContainerStateTerminated) string {
	parts := []string{fmt.Sprintf("the data mover exited with code %d", terminated.ExitCode)}

	if terminated.Signal != 0 {
		parts = append(parts, fmt.Sprintf("killed by signal %d", terminated.Signal))
	}

	if terminated.Reason != "" && terminated.Reason != genericTerminationReason {
		parts = append(parts, "reason: "+terminated.Reason)
	}

	if message := strings.TrimSpace(terminated.Message); message != "" {
		parts = append(parts, "message: "+message)
	}

	joined := strings.Join(parts, ", ")

	// Parenthesized rather than comma-joined, so the attribution reads as an
	// aside to the observed fact instead of splicing into it.
	if meaning := interpretExitCode(jobName, int(terminated.ExitCode)); meaning != "" {
		joined += " (" + meaning + ")"
	}

	return joined
}

// genericTerminationReason is what Kubernetes sets for any non-zero exit.
const genericTerminationReason = "Error"

// podStatusDetail is what is left to say when no container ever terminated, which
// is how an evicted or admission-rejected pod looks.
func podStatusDetail(pod *corev1.Pod) string {
	var parts []string

	if pod.Status.Reason != "" {
		parts = append(parts, "reason: "+pod.Status.Reason)
	}

	if message := strings.TrimSpace(pod.Status.Message); message != "" {
		parts = append(parts, "message: "+message)
	}

	if len(parts) == 0 {
		return ""
	}

	return ": " + strings.Join(parts, ", ")
}

// terminatedContainerState returns the terminated state of the data mover's
// container, selected by name rather than by position. Only a job whose data
// mover cannot be named falls back to any terminated container: in a pod with
// injected sidecars, another container's exit code presented as the data
// mover's would be a fabricated cause.
func terminatedContainerState(pod *corev1.Pod, jobName string) *corev1.ContainerStateTerminated {
	name := dataMoverContainer(jobName)

	if name != "" {
		for i := range pod.Status.ContainerStatuses {
			status := &pod.Status.ContainerStatuses[i]
			if status.Name == name && status.State.Terminated != nil {
				return status.State.Terminated
			}
		}

		return nil
	}

	for i := range pod.Status.ContainerStatuses {
		if terminated := pod.Status.ContainerStatuses[i].State.Terminated; terminated != nil {
			return terminated
		}
	}

	return nil
}

// DescribeJobFailure returns the explanation for a failed job's pod, or an
// empty string when there is nothing more to say than the job status itself.
func DescribeJobFailure(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job) string {
	pod, _, err := findTerminalJobPod(ctx, cli, job.Namespace, job.Name)
	if err != nil || pod == nil || pod.Status.Phase != corev1.PodFailed {
		return ""
	}

	return failedPodError(ctx, cli, job.Name, pod).Error()
}

// WriteJobFailureEvidence prints a failed job's evidence: the pod's log tail
// and what the cluster reports about the release's resources. Best effort, for
// the status command, whose detached operations are the primary consumers of a
// failure explanation.
func WriteJobFailureEvidence(
	ctx context.Context,
	cli kubernetes.Interface,
	job *batchv1.Job,
	structuredLogs bool,
	palette console.Palette,
	writer io.Writer,
	logger *slog.Logger,
) {
	if pod, _, err := findTerminalJobPod(ctx, cli, job.Namespace, job.Name); err == nil && pod != nil {
		writeFailureTail(ctx, cli, job.Name, pod, structuredLogs, palette, writer, logger)
	}

	release := job.Name
	for _, suffix := range jobSuffixes {
		release = strings.TrimSuffix(release, suffix)
	}

	if structuredLogs {
		var buf bytes.Buffer

		WriteWorkloadDiagnostics(
			ctx,
			cli,
			job.Namespace,
			InstanceLabelSelector(release),
			console.Palette{},
			&buf,
			logger,
		)
		logger.Error("❌ What the cluster reported", "release", release,
			"namespace", job.Namespace, "diagnostics", buf.String())

		return
	}

	fmt.Fprintf(writer, "%s\n\n  %s (namespace %s):\n",
		palette.Bold("What the cluster reported:"), release, job.Namespace)
	WriteWorkloadDiagnostics(ctx, cli, job.Namespace, InstanceLabelSelector(release), palette, writer, logger)
	fmt.Fprintln(writer)
}

// dataMoverContainer is the container the chart names after the data mover it
// runs, picked by the job-name suffix the same way the progress parser is.
func dataMoverContainer(jobName string) string {
	switch {
	case strings.HasSuffix(jobName, rsyncJobSuffix):
		return "rsync"
	case strings.HasSuffix(jobName, rcloneJobSuffix):
		return "rclone"
	default:
		return ""
	}
}

// interpretExitCode returns the data mover's own documented meaning for an exit
// status. The tables live next to what they describe.
func interpretExitCode(jobName string, code int) string {
	switch {
	case strings.HasSuffix(jobName, rsyncJobSuffix):
		return rsync.Interpret(code)
	case strings.HasSuffix(jobName, rcloneJobSuffix):
		return rclone.Interpret(code)
	default:
		return ""
	}
}
