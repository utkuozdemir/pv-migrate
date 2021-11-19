package k8s

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func WaitForJobCompletion(logger *log.Entry, cli kubernetes.Interface,
	ns string, name string, progressBarRequested bool) error {
	pod, err := waitForJobPod(cli, ns, name)
	if err != nil {
		return err
	}

	successCh := make(chan bool, 1)

	showProgressBar := progressBarRequested &&
		logger.Context.Value(applog.FormatContextKey) == applog.FormatFancy
	go handlePodLogs(cli, ns, pod.Name, successCh, showProgressBar, logger)

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
