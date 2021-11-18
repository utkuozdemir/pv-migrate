package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	podWatchTimeout = 2 * time.Minute
)

func waitForJobPod(cli kubernetes.Interface, ns string, jobName string) (*corev1.Pod, error) {
	watch, err := cli.CoreV1().Pods(ns).Watch(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})

	if err != nil {
		return nil, err
	}

	timeoutCh := time.After(podWatchTimeout)
	for {
		select {
		case event := <-watch.ResultChan():
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				return nil, fmt.Errorf("unexpected type while watcing pods "+
					"in ns %s with job-name=%s", ns, jobName)
			}

			phase := pod.Status.Phase
			if phase != corev1.PodPending {
				return pod, nil
			}
		case <-timeoutCh:
			return nil, fmt.Errorf("timed out waiting for the job pod %s/%s", ns, jobName)
		}
	}
}

func waitForPodTermination(cli kubernetes.Interface, ns string, name string) (*corev1.PodPhase, error) {
	watch, err := cli.CoreV1().Pods(ns).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})
	if err != nil {
		return nil, err
	}

	timeoutCh := time.After(podWatchTimeout)
	for {
		select {
		case event := <-watch.ResultChan():
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				return nil, fmt.Errorf("unexpected type while watcing pod %s/%s", ns, name)
			}

			phase := pod.Status.Phase
			if phase != corev1.PodRunning {
				return &phase, nil
			}
		case <-timeoutCh:
			return nil, fmt.Errorf(
				"timed out waiting for termination of pod %s/%s", ns, name)
		}
	}
}
