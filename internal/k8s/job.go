package k8s

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sync"
)

func WaitForJobCompletion(logger *log.Entry, cli kubernetes.Interface,
	ns string, name string, showProgressBar bool) error {
	pod, err := waitForJobPod(cli, ns, name)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()
	successCh := make(chan bool, 1)

	go tryLogProgressFromRsyncLogs(&wg, cli, pod, successCh, showProgressBar, logger)
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
