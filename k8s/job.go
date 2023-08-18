package k8s

import (
	"context"
	"fmt"
	"io"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	applog "github.com/utkuozdemir/pv-migrate/log"
	"github.com/utkuozdemir/pv-migrate/rsync"
)

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
			stream, streamErr := cli.CoreV1().Pods(namespace).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(context.TODO())
			if streamErr != nil {
				return nil, fmt.Errorf("failed to stream logs from pod %s/%s: %w", namespace, pod.Name, streamErr)
			}

			return stream, nil
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

		return fmt.Errorf("job %s/%s failed", pod.Namespace, pod.Name)
	}

	successCh <- true

	return nil
}
