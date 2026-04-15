package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/jobprogress"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

func buildStatusCmd(logger **slog.Logger) *cobra.Command {
	var (
		kubeconfig  string
		kubeContext string
		namespace   string
		follow      bool
	)

	cmd := &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Show the status of a detached operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "" {
				return errors.New("operation ID must not be empty")
			}

			return runStatus(cmd.Context(), *logger, kubeconfig, kubeContext, namespace, args[0], follow)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	flags.StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	flags.StringVarP(&namespace, "namespace", "n", "", "Namespace to search (default: all namespaces)")
	flags.BoolVarP(&follow, "follow", "f", false, "Follow operation progress")

	return cmd
}

func runStatus(
	ctx context.Context, logger *slog.Logger, kubeconfig, kubeContext, namespace, operationID string, follow bool,
) error {
	client, err := k8s.GetClusterClient(kubeconfig, kubeContext, logger)
	if err != nil {
		return err
	}

	ns := namespace
	if ns == "" {
		ns = client.NsInContext
	}

	releasePrefix := helmReleasePrefix + operationID + "-"

	job, err := k8s.FindDataMoverJob(ctx, client.KubeClient, ns, releasePrefix, logger)
	if err != nil {
		return err
	}

	if follow {
		if job.Status.Succeeded > 0 || job.Status.Failed > 0 {
			k8s.WriteRecentJobPodLogs(ctx, client.KubeClient, job, os.Stderr, logger)
			printJobStatus(job, logger)

			if job.Status.Failed > 0 {
				return fmt.Errorf("failed to follow progress: job %s/%s failed", job.Namespace, job.Name)
			}

			return nil
		}

		err = followJobProgress(ctx, client.KubeClient, job, logger)
		job = refreshJob(ctx, client.KubeClient, job, logger)
		printJobStatus(job, logger)

		return err
	}

	if job.Status.Active > 0 {
		printJobProgress(ctx, client.KubeClient, job, logger)
	}

	printJobStatus(job, logger)

	return nil
}

func followJobProgress(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job, logger *slog.Logger) error {
	logger.Info("Following job progress", "job", job.Name, "type", jobprogress.Description(job.Name))

	if err := k8s.WaitForJobCompletion(ctx, cli, job.Namespace, job.Name, true, os.Stderr, logger); err != nil {
		return fmt.Errorf("failed to follow progress: %w", err)
	}

	return nil
}

func refreshJob(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job, logger *slog.Logger) *batchv1.Job {
	refreshed, err := cli.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		logger.Debug("failed to refresh job status", "job", job.Namespace+"/"+job.Name, "error", err)

		return job
	}

	return refreshed
}

func printJobProgress(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job, logger *slog.Logger) {
	pod, err := k8s.FindJobPod(ctx, cli, job)
	if err != nil {
		return
	}

	tailLines := int64(5) //nolint:mnd

	stream, err := cli.CoreV1().Pods(job.Namespace).GetLogs(pod.Name,
		&corev1.PodLogOptions{TailLines: &tailLines}).Stream(ctx)
	if err != nil {
		return
	}

	defer stream.Close()

	data, err := io.ReadAll(stream)
	if err != nil {
		return
	}

	latest, ok := jobprogress.FindLast(job.Name, string(data))
	if !ok {
		return
	}

	logger.Info("Operation progress",
		"percentage", fmt.Sprintf("%d%%", latest.Percentage),
		"transferred", formatBytes(latest.Transferred),
		"total", formatBytes(latest.Total),
	)
}

func printJobStatus(job *batchv1.Job, logger *slog.Logger) {
	var status string

	switch {
	case job.Status.Succeeded > 0:
		status = "Succeeded"
	case job.Status.Failed > 0:
		status = "Failed"
	case job.Status.Active > 0:
		status = "Running"
	default:
		status = "Pending"
	}

	var elapsed string

	if job.Status.StartTime != nil {
		end := time.Now()
		if job.Status.CompletionTime != nil {
			end = job.Status.CompletionTime.Time
		}

		elapsed = end.Sub(job.Status.StartTime.Time).Truncate(time.Second).String()
	}

	logger.Info("Operation status",
		"job", job.Name,
		"namespace", job.Namespace,
		"status", status,
		"elapsed", elapsed,
	)
}

func formatBytes(bytes int64) string {
	const unit = 1024

	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0

	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
