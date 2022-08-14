package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

var ErrJobFailed = errors.New("job failed")

//nolint:cyclop
func WaitForJobCompletion(
	ctx context.Context,
	logger *log.Entry,
	cli kubernetes.Interface,
	namespace string,
	name string,
	progressBarRequested bool,
) error {
	s := fmt.Sprintf("job-name=%s", name)

	pod, err := WaitForPod(ctx, cli, namespace, s)
	if err != nil {
		return err
	}

	successCh := make(chan bool, 1)

	showProgressBar := progressBarRequested &&
		logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy

	logTail := rsync.LogTail{
		LogReaderFunc: func() (io.ReadCloser, error) {
			return cli.CoreV1().Pods(namespace).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(ctx)
		},
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          logger,
	}

	go logTail.Start(ctx)

	terminatedPod, err := waitForPodTermination(ctx, cli, pod.Namespace, pod.Name)
	if err != nil {
		select {
		case successCh <- false:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if *terminatedPod != corev1.PodSucceeded {
		select {
		case successCh <- false:
		case <-ctx.Done():
			return ctx.Err()
		}

		err := fmt.Errorf("%w: %s", ErrJobFailed, name)

		return err
	}

	select {
	case successCh <- true:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}
