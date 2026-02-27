package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

// WaitForJobCompletion waits for the Kubernetes job to complete.
func WaitForJobCompletion(ctx context.Context, cli kubernetes.Interface,
	ns, name string, showProgressBar bool, writer io.Writer, logger *slog.Logger,
) (retErr error) {
	labelSelector := "job-name=" + name

	pod, err := WaitForPod(ctx, cli, ns, labelSelector)
	if err != nil {
		return err
	}

	var eg errgroup.Group

	defer func() {
		retErr = errors.Join(retErr, eg.Wait())
	}()

	tailCtx, tailCancel := context.WithCancel(ctx)
	defer tailCancel()

	progressLogger := progress.NewLogger(progress.LoggerOptions{
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
		return fmt.Errorf("job %s/%s failed", pod.Namespace, pod.Name)
	}

	if err = progressLogger.MarkAsComplete(ctx); err != nil {
		return fmt.Errorf("failed to mark progress logger as complete: %w", err)
	}

	return nil
}
