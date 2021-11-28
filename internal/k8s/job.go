package k8s

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func WaitForJobCompletion(logger *log.Entry, cli kubernetes.Interface,
	ns string, name string, progressBarRequested bool) error {
	s := fmt.Sprintf("job-name=%s", name)
	pod, err := WaitForPod(cli, ns, s)
	if err != nil {
		return err
	}

	successCh := make(chan bool, 1)

	showProgressBar := progressBarRequested &&
		logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy

	l := rsync.LogTail{
		LogReaderFunc: func() (io.ReadCloser, error) {
			return cli.CoreV1().Pods(ns).GetLogs(pod.Name,
				&corev1.PodLogOptions{Follow: true}).Stream(context.TODO())
		},
		SuccessCh:       successCh,
		ShowProgressBar: showProgressBar,
		Logger:          logger,
	}

	go l.Start()

	p, err := waitForPodTermination(cli, pod.Namespace, pod.Name)
	if err != nil {
		successCh <- false
		return err
	}

	if *p != corev1.PodSucceeded {
		successCh <- false
		err := fmt.Errorf("job %s failed", name)
		return err
	}

	successCh <- true
	return nil
}
