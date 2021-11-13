package k8s

import (
	"context"
	"errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	serviceLbCheckIntervalSeconds = 5
	serviceLbCheckInterval        = 5 * time.Second
	serviceLbCheckTimeoutSeconds  = 120
	serviceLbCheckTimeout         = 120 * time.Second
)

func GetServiceAddress(logger *log.Entry, kubeClient kubernetes.Interface, serviceNs string, serviceName string) (string, error) {
	svc, err := kubeClient.CoreV1().Services(serviceNs).Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if svc.Spec.Type == corev1.ServiceTypeClusterIP {
		return svc.Name + "." + svc.Namespace, nil
	}

	services := kubeClient.CoreV1().Services(svc.Namespace)
	getOptions := metav1.GetOptions{}
	timeout := time.After(serviceLbCheckTimeout)
	ticker := time.NewTicker(serviceLbCheckInterval)
	elapsedSecs := 0
	for {
		select {
		case <-timeout:
			return "", errors.New("timed out waiting for the LoadBalancer svc address")

		case <-ticker.C:
			elapsedSecs += serviceLbCheckIntervalSeconds
			lbService, err := services.Get(context.TODO(), svc.Name, getOptions)
			if err != nil {
				return "", err
			}

			if len(lbService.Status.LoadBalancer.Ingress) > 0 {
				return lbService.Status.LoadBalancer.Ingress[0].IP, nil
			}

			logger.WithField("svc", svc.Name).
				WithField("elapsedSecs", elapsedSecs).
				WithField("intervalSecs", serviceLbCheckIntervalSeconds).
				WithField("timeoutSecs", serviceLbCheckTimeoutSeconds).
				Info(":hourglass_not_done: Waiting for LoadBalancer IP")
		}
	}
}
