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

func GetServiceAddress(kubeClient kubernetes.Interface, service *corev1.Service) (string, error) {
	if service.Spec.Type == corev1.ServiceTypeClusterIP {
		return service.Name + "." + service.Namespace, nil
	}

	services := kubeClient.CoreV1().Services(service.Namespace)
	getOptions := metav1.GetOptions{}
	timeout := time.After(serviceLbCheckTimeout)
	ticker := time.NewTicker(serviceLbCheckInterval)
	elapsedSecs := 0
	for {
		select {
		case <-timeout:
			return "", errors.New("timed out waiting for the LoadBalancer service address")

		case <-ticker.C:
			elapsedSecs += serviceLbCheckIntervalSeconds
			lbService, err := services.Get(context.TODO(), service.Name, getOptions)
			if err != nil {
				return "", err
			}

			if len(lbService.Status.LoadBalancer.Ingress) > 0 {
				return lbService.Status.LoadBalancer.Ingress[0].IP, nil
			}

			log.WithField("service", service.Name).
				WithField("elapsedSecs", elapsedSecs).
				WithField("intervalSecs", serviceLbCheckIntervalSeconds).
				WithField("timeoutSecs", serviceLbCheckTimeoutSeconds).
				Info("Waiting for LoadBalancer IP")
		}
	}
}
