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
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

func buildStatusCmd(logger **slog.Logger) *cobra.Command {
	var (
		kubeconfig  string
		kubeContext string
		namespace   string
		follow      bool
	)

	cmd := &cobra.Command{
		Use:   "status <migration-id>",
		Short: "Show the status of a detached migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "" {
				return errors.New("migration ID must not be empty")
			}

			return runStatus(cmd.Context(), *logger, kubeconfig, kubeContext, namespace, args[0], follow)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	flags.StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	flags.StringVarP(&namespace, "namespace", "n", "", "Namespace to search (default: all namespaces)")
	flags.BoolVarP(&follow, "follow", "f", false, "Follow rsync progress")

	return cmd
}

func runStatus(
	ctx context.Context, logger *slog.Logger, kubeconfig, kubeContext, namespace, migrationID string, follow bool,
) error {
	client, err := k8s.GetClusterClient(kubeconfig, kubeContext, logger)
	if err != nil {
		return err
	}

	ns := namespace
	if ns == "" {
		ns = client.NsInContext
	}

	releasePrefix := helmReleasePrefix + migrationID + "-"

	job, err := k8s.FindDataMoverJob(ctx, client.KubeClient, ns, releasePrefix)
	if err != nil {
		return err
	}

	if job.Status.Active > 0 {
		if follow {
			return followJobProgress(ctx, client.KubeClient, job, logger)
		}

		printJobProgress(ctx, client.KubeClient, job, logger)
	}

	printJobStatus(job, logger)

	return nil
}

func followJobProgress(ctx context.Context, cli kubernetes.Interface, job *batchv1.Job, logger *slog.Logger) error {
	pod, err := k8s.FindJobPod(ctx, cli, job)
	if err != nil {
		return fmt.Errorf("find job pod: %w", err)
	}

	logger.Info("Following rsync progress", "pod", pod.Name)

	progressLogger := progress.NewLogger(progress.LoggerOptions{
		Writer:          os.Stderr,
		ShowProgressBar: true,
		LogStreamFunc: func(ctx context.Context) (io.ReadCloser, error) {
			return cli.CoreV1().Pods(job.Namespace).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(ctx)
		},
	})

	if err = progressLogger.Start(ctx, logger); err != nil {
		return fmt.Errorf("failed to follow progress: %w", err)
	}

	return nil
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

	latest := progress.FindLast(string(data))
	logger.Info("Migration progress",
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

	logger.Info("Migration status",
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
