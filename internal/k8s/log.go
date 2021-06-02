package k8s

import (
	"context"
	log "github.com/sirupsen/logrus"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func tailPodLogs(logger *log.Entry, kubeClient kubernetes.Interface, namespace string, pod string, stopCh <-chan bool) {
	logger = logger.WithFields(log.Fields{
		"ns":  namespace,
		"pod": pod,
	})

	podLogOptions := corev1.PodLogOptions{
		Follow: true,
	}

	podLogRequest := kubeClient.CoreV1().Pods(namespace).GetLogs(pod, &podLogOptions)
	stream, err := podLogRequest.Stream(context.TODO())
	if err != nil {
		logger.WithError(err).Errorf("Failed to tail logs")
	}

	defer func(stream io.ReadCloser) {
		err := stream.Close()
		if err != nil {
			logger.WithError(err).Errorf("Failed to close log tail stream")
		}
	}(stream)

	for {
		select {
		case <-stopCh:
			return
		default:
			buf := make([]byte, 2000)
			numBytes, err := stream.Read(buf)
			if numBytes == 0 {
				continue
			}

			if err == io.EOF {
				break
			}

			if err != nil {
				logger.WithError(err).Errorf("Failed real from log tail stream")
			}
			message := string(buf[:numBytes])
			logger.Info(message)
		}
	}
}
