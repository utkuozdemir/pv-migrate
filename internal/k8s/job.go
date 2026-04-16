package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"golang.org/x/sync/errgroup"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/jobprogress"
	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

// FindJobPod returns a pod for the given job, preferring a Running pod.
func FindJobPod(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job) (*corev1.Pod, error) {
	pods, err := cli.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + job.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods for job %s: %w", job.Name, err)
	}

	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			return &pods.Items[i], nil
		}
	}

	if len(pods.Items) > 0 {
		return &pods.Items[0], nil
	}

	return nil, fmt.Errorf("no pods found for job %s", job.Name)
}

// jobSuffixes are the suffixes used by pv-migrate Helm chart job names.
var jobSuffixes = []string{"-rsync", "-rclone"}

// FindDataMoverJob finds the data-mover job (rsync or rclone) for a migration by listing
// all Helm-managed jobs and matching by the release name prefix plus a known suffix.
// If nothing is found in the given namespace, it retries across all namespaces.
func FindDataMoverJob(ctx context.Context, cli kubernetes.Interface, ns, releasePrefix string) (*batchv1.Job, error) {
	jobs, err := cli.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=Helm",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	for i := range jobs.Items {
		job := &jobs.Items[i]
		if !strings.HasPrefix(job.Name, releasePrefix) {
			continue
		}

		for _, suffix := range jobSuffixes {
			if strings.HasSuffix(job.Name, suffix) {
				return job, nil
			}
		}
	}

	if ns != "" {
		return FindDataMoverJob(ctx, cli, "", releasePrefix)
	}

	return nil, fmt.Errorf("no job found for migration %s", releasePrefix)
}

// WaitForJobStart waits until the job's pod transitions out of the Pending phase.
// It returns the pod object once the pod is running (or has already terminated).
func WaitForJobStart(ctx context.Context, cli kubernetes.Interface,
	ns, name string, logger *slog.Logger,
) (*corev1.Pod, error) {
	labelSelector := "job-name=" + name

	logger.Info("⏳ Waiting for job pod to start", "job", name)

	pod, err := WaitForPod(ctx, cli, ns, labelSelector)
	if err != nil {
		return nil, err
	}

	switch pod.Status.Phase { //nolint:exhaustive
	case corev1.PodRunning:
		logger.Info("🏃 Job pod is running", "pod", pod.Name)
	case corev1.PodSucceeded, corev1.PodFailed:
		logger.Info("🏁 Job pod has already completed", "pod", pod.Name, "phase", pod.Status.Phase)
	default:
		logger.Info("✅ Job pod has started", "pod", pod.Name, "phase", pod.Status.Phase)
	}

	return pod, nil
}

// WaitForJobCompletion waits for the Kubernetes job to complete.
func WaitForJobCompletion(ctx context.Context, cli kubernetes.Interface,
	ns, name string, showProgressBar bool, writer io.Writer, logger *slog.Logger,
) (retErr error) {
	pod, terminal, err := findTerminalJobPod(ctx, cli, ns, name)
	if err != nil {
		return err
	}

	if terminal {
		return ensurePodSucceeded(pod)
	}

	pod, err = WaitForJobStart(ctx, cli, ns, name, logger)
	if err != nil {
		return err
	}

	if pod.Status.Phase != corev1.PodRunning {
		return ensurePodSucceeded(pod)
	}

	var eg errgroup.Group

	defer func() {
		retErr = errors.Join(retErr, eg.Wait())
	}()

	tailCtx, tailCancel := context.WithCancel(ctx)
	defer tailCancel()

	progressLogger := jobprogress.NewLogger(name, progresslog.LoggerOptions{
		Writer:          writer,
		ShowProgressBar: showProgressBar,
		LogStreamFunc: func(ctx context.Context) (io.ReadCloser, error) {
			return cli.CoreV1().Pods(ns).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(ctx)
		},
	})

	eg.Go(func() error {
		return progressLogger.Start(tailCtx, logger)
	})

	phase, err := waitForPodTermination(ctx, cli, pod.Namespace, pod.Name)
	if err != nil {
		return err
	}

	if *phase != corev1.PodSucceeded {
		return failedPodError(pod)
	}

	if err = progressLogger.MarkAsComplete(ctx); err != nil {
		return fmt.Errorf("failed to mark progress logger as complete: %w", err)
	}

	return nil
}

func findTerminalJobPod(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, jobName string,
) (*corev1.Pod, bool, error) {
	pods, err := cli.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
	}

	var terminal *corev1.Pod

	for idx := range pods.Items {
		switch pods.Items[idx].Status.Phase { //nolint:exhaustive
		case corev1.PodPending, corev1.PodRunning:
			return nil, false, nil
		case corev1.PodSucceeded:
			terminal = &pods.Items[idx]
		case corev1.PodFailed:
			if terminal == nil {
				terminal = &pods.Items[idx]
			}
		}
	}

	return terminal, terminal != nil, nil
}

func ensurePodSucceeded(pod *corev1.Pod) error {
	if pod.Status.Phase != corev1.PodSucceeded {
		return failedPodError(pod)
	}

	return nil
}

func failedPodError(pod *corev1.Pod) error {
	return fmt.Errorf("job %s/%s failed", pod.Namespace, pod.Name)
}
