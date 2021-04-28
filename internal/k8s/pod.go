package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	podPollInterval = 2 * time.Second
	podPollTimeout  = 2 * time.Minute
	podRunTimeout   = 24 * time.Hour
)

func waitUntilPodIsScheduled(kubeClient kubernetes.Interface, namespace string, pod string) error {
	return wait.PollImmediate(podPollInterval, podPollTimeout, func() (done bool, err error) {
		p, err := getPodPhase(kubeClient, namespace, pod)
		if err != nil {
			return false, err
		}

		phase := *p
		if phase == corev1.PodPending {
			return false, nil
		}

		if phase == corev1.PodRunning || phase == corev1.PodFailed || phase == corev1.PodSucceeded {
			return true, nil
		}

		return false, fmt.Errorf("Pod in unexpected phase: %v", phase)
	})
}

func waitUntilPodIsNotRunning(kubeClient kubernetes.Interface, namespace string, pod string) (*corev1.PodPhase, error) {
	var p *corev1.PodPhase
	err := wait.PollImmediate(podPollInterval, podRunTimeout, func() (done bool, err error) {
		p, err = getPodPhase(kubeClient, namespace, pod)
		if err != nil {
			return false, err
		}

		if *p == corev1.PodRunning {
			return false, nil
		}

		return true, nil
	})
	return p, err
}

func getPodPhase(kubeClient kubernetes.Interface, namespace string, pod string) (*corev1.PodPhase, error) {
	p, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), pod, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &p.Status.Phase, nil
}
