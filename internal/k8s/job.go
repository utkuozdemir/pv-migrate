package k8s

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	jobPodPollInterval = 2 * time.Second
	jobPodSpawnTimeout = 5 * time.Minute
)

func CreateJobWaitTillCompleted(logger *log.Entry, kubeClient kubernetes.Interface, job *batchv1.Job) error {
	_, err := kubeClient.BatchV1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	pod, err := waitUntilJobPodIsCreated(kubeClient, job.Namespace, job.Name)
	if err != nil {
		return err
	}

	err = waitUntilPodIsScheduled(kubeClient, pod.Namespace, pod.Name)
	if err != nil {
		return err
	}

	stopCh := make(chan bool)
	go tailPodLogs(logger, kubeClient, pod.Namespace, pod.Name, stopCh)
	p, err := waitUntilPodIsNotRunning(kubeClient, pod.Namespace, pod.Name)
	if err != nil {
		return err
	}
	stopCh <- true
	if *p != corev1.PodSucceeded {
		return fmt.Errorf("job %s failed", job.Name)
	}

	return nil
}

func waitUntilJobPodIsCreated(kubeClient kubernetes.Interface, namespace string, job string) (*corev1.Pod, error) {
	pods := kubeClient.CoreV1().Pods(namespace)
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", job),
	}

	var pod *corev1.Pod
	err := wait.PollImmediate(jobPodPollInterval, jobPodSpawnTimeout, func() (done bool, err error) {
		podList, err := pods.List(context.TODO(), listOptions)
		if err != nil {
			return false, err
		}

		if len(podList.Items) > 0 {
			pod = &podList.Items[0]
			return true, nil
		}

		return false, nil
	})

	return pod, err
}
