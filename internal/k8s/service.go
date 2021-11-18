package k8s

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"time"
)

const (
	serviceLbCheckIntervalSeconds = 5
	serviceLbCheckInterval        = 5 * time.Second
	serviceLbCheckTimeoutSeconds  = 120
	serviceLbCheckTimeout         = 120 * time.Second
)

func GetServiceAddress(logger *log.Entry, cli kubernetes.Interface, ns string, name string) (string, error) {
	svc, err := cli.CoreV1().Services(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if svc.Spec.Type == corev1.ServiceTypeClusterIP {
		return svc.Name + "." + svc.Namespace, nil
	}

	services := cli.CoreV1().Services(svc.Namespace)

	watch, err := services.Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})
	if err != nil {
		return "", err
	}

	elapsedSecs := 0
	ticker := time.NewTicker(serviceLbCheckInterval)
	timeoutCh := time.After(serviceLbCheckTimeout)
	for {
		select {
		case event := <-watch.ResultChan():
			svc, ok := event.Object.(*corev1.Service)
			if !ok {
				return "", fmt.Errorf("unexpected type while watcing services in ns %s", ns)
			}

			if len(svc.Status.LoadBalancer.Ingress) > 0 {
				return svc.Status.LoadBalancer.Ingress[0].IP, nil
			}
		case <-timeoutCh:
			return "", fmt.Errorf("timed out waiting for the "+
				"LoadBalancer svc address in namespace %s/%s", ns, name)
		case <-ticker.C:
			logger.WithField("svc", svc.Name).
				WithField("elapsedSecs", elapsedSecs).
				WithField("intervalSecs", serviceLbCheckIntervalSeconds).
				WithField("timeoutSecs", serviceLbCheckTimeoutSeconds).
				Info(":hourglass_not_done: Waiting for LoadBalancer IP")
		}
	}
}
