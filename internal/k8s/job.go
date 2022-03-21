package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"

	log "github.com/sirupsen/logrus"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

var ErrJobFailed = errors.New("job failed")

func WaitForJobCompletion(logger *log.Entry, cli kubernetes.Interface,
	namespace string, name string, progressBarRequested bool,
) error {
	s := fmt.Sprintf("job-name=%s", name)

	pod, err := WaitForPod(cli, namespace, s)
	if err != nil {
		return err
	}

	successCh := make(chan bool, 1)

	showProgressBar := progressBarRequested &&
		logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy

	logTail := rsync.LogTail{
		LogReaderFunc: func() (io.ReadCloser, error) {
			return cli.CoreV1().Pods(namespace).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(context.TODO())
		},
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          logger,
	}

	go logTail.Start()

	terminatedPod, err := waitForPodTermination(cli, pod.Namespace, pod.Name)
	if err != nil {
		successCh <- false

		return err
	}

	if *terminatedPod != corev1.PodSucceeded {
		successCh <- false

		err := fmt.Errorf("%w: %s", ErrJobFailed, name)

		return err
	}

	successCh <- true

	return nil
}
