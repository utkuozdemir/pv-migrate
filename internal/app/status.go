package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/jobprogress"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/opid"
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

			return runStatus(cmd.Context(), *logger, kubeconfig, kubeContext, namespace, args[0], follow,
				structuredLogsRequested(cmd))
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
	ctx context.Context, logger *slog.Logger, kubeconfig, kubeContext, namespace, operationID string,
	follow, structuredLogs bool,
) error {
	client, err := k8s.GetClusterClient(kubeconfig, kubeContext, logger)
	if err != nil {
		return err
	}

	ns := namespace
	if ns == "" {
		ns = client.NsInContext
	}

	releasePrefix := opid.ReleasePrefix + operationID + "-"

	job, err := k8s.FindDataMoverJob(ctx, client.KubeClient, ns, releasePrefix, logger)
	if err != nil {
		return err
	}

	if follow {
		// No special case for an already-finished job: the waiter handles a
		// terminal pod, and it is the only path that explains the exit code.
		err = followJobProgress(ctx, client.KubeClient, job, structuredLogs, logger)
		job = refreshJob(ctx, client.KubeClient, job, logger)
		printJobStatus(job, logger)

		return err
	}

	if job.Status.Active > 0 {
		printJobProgress(ctx, client.KubeClient, job, logger)
	}

	// A failed operation is why status gets run at all, so the one-line status
	// alone would bury the answer the pod still holds. The status line leads, so
	// the evidence below it has context, then the explanation closes, and the
	// exit code is non-zero so automation sees the failure too.
	if job.Status.Failed > 0 {
		printJobStatus(job, logger)

		palette := console.Palette{Enabled: console.ForTerminal(isatty.IsTerminal(os.Stderr.Fd()), structuredLogs)}
		explanation := k8s.DescribeJobFailure(ctx, client.KubeClient, job)

		// The migration summary's shape: the verdict with its cause first, the
		// evidence below it. In structured mode the explanation travels as a
		// record instead, and the evidence follows the same rule.
		if structuredLogs {
			if explanation != "" {
				logger.Error("❌ Operation failed", "error", explanation)
			}
		} else {
			fmt.Fprintf(os.Stderr, "\n%s\n", palette.Failure("Operation failed."))

			for line := range strings.SplitSeq(explanation, "\n") {
				fmt.Fprintf(os.Stderr, "    %s\n", line)
			}
		}

		k8s.WriteJobFailureEvidence(ctx, client.KubeClient, job, structuredLogs, palette, os.Stderr, logger)

		return fmt.Errorf("operation %s failed", operationID)
	}

	printJobStatus(job, logger)

	return nil
}

func followJobProgress(
	ctx context.Context, cli kubernetes.Interface, job *batchv1.Job, structuredLogs bool, logger *slog.Logger,
) error {
	logger.Info("Following job progress", "job", job.Name, "type", jobprogress.Description(job.Name))

	palette := console.Palette{Enabled: console.ForTerminal(isatty.IsTerminal(os.Stderr.Fd()), structuredLogs)}

	if err := k8s.WaitForJobCompletion(ctx, cli, job.Namespace, job.Name, true, structuredLogs,
		palette, os.Stderr, logger); err != nil {
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

	elapsed := jobElapsed(job)

	// The level carries the state: a failed operation announced at a green INFO
	// reads as fine on a skim.
	logFn := logger.Info

	switch {
	case job.Status.Failed > 0:
		logFn = logger.Error
	case job.Status.Succeeded == 0 && job.Status.Active == 0:
		logFn = logger.Warn
	}

	logFn("Operation status",
		"job", job.Name,
		"namespace", job.Namespace,
		"status", status,
		"elapsed", elapsed,
	)
}

// jobElapsed reports how long the job ran. A failed job has no completion
// time, and "now minus start" on one that failed days ago reads as a days-long
// run, so the failure condition's transition time is the end when it is there.
func jobElapsed(job *batchv1.Job) string {
	if job.Status.StartTime == nil {
		return ""
	}

	end := time.Now()

	switch {
	case job.Status.CompletionTime != nil:
		end = job.Status.CompletionTime.Time
	case job.Status.Failed > 0:
		for i := range job.Status.Conditions {
			cond := &job.Status.Conditions[i]
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue &&
				!cond.LastTransitionTime.IsZero() {
				end = cond.LastTransitionTime.Time

				break
			}
		}
	}

	return end.Sub(job.Status.StartTime.Time).Truncate(time.Second).String()
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
