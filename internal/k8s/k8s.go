package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"time"
)

const (
	serviceLbCheckIntervalSeconds = 5
	serviceLbCheckInterval        = serviceLbCheckIntervalSeconds * time.Second
	serviceLbCheckTimeoutSeconds  = 120
	serviceLbCheckTimeout         = serviceLbCheckTimeoutSeconds * time.Second
)

type podResult struct {
	success bool
	pod     *corev1.Pod
}

func GetServiceAddress(service *corev1.Service, kubeClient kubernetes.Interface) (string, error) {
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

func CreateJobWaitTillCompleted(kubeClient kubernetes.Interface, job *batchv1.Job) error {
	channel := make(chan podResult)
	defer close(channel)

	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == job.Namespace && newPod.Labels["job-name"] == job.Name {
					switch newPod.Status.Phase {
					case corev1.PodSucceeded:
						log.WithFields(log.Fields{
							"job": job.Name,
							"pod": newPod.Name,
						}).Info("Job completed")
						channel <- podResult{true, newPod}
					case corev1.PodRunning:
						log.WithFields(log.Fields{
							"job": job.Name,
							"pod": newPod.Name,
						}).Info("Job running")
					case corev1.PodFailed, corev1.PodUnknown:
						channel <- podResult{false, newPod}
					}
				}
			},
		},
	)

	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory.Start(stopCh)

	log.WithFields(log.Fields{
		"job": job.Name,
	}).Info("Creating rsync job")
	_, err := kubeClient.BatchV1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"job": job.Name,
	}).Info("Waiting for rsync job to finish")

	result := <-channel
	if !result.success {
		pod := result.pod
		logs, err := getPodLogs(kubeClient, pod, 10)
		if err != nil {
			return fmt.Errorf("couldn't get logs for the pod of the failed job: %w", err)
		}
		return fmt.Errorf("job failed with pod logs: %v", logs)
	}
	return nil
}

func getPodLogs(kubeClient kubernetes.Interface, pod *corev1.Pod, lines int64) (string, error) {
	podLogOpts := corev1.PodLogOptions{
		TailLines: &lines,
	}
	req := kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(context.TODO())
	if err != nil {
		return "", err
	}

	defer func() {
		err = podLogs.Close()
	}()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}
	str := buf.String()
	return str, nil
}
