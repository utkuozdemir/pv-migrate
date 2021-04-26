package integrationtest

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
)

func ensurePodIsDeleted(kubeClient kubernetes.Interface, namespace string, name string) error {
	podLogger := log.WithFields(log.Fields{
		"namespace": namespace,
		"pod":       name,
	})

	podLogger.Info("Deleting pod")
	err := kubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if kubeerrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		_, err = kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if kubeerrors.IsNotFound(err) {
			return true, nil
		}

		if err != nil {
			return false, err
		}

		podLogger.Info("Pod is still there, polling...")
		return false, nil
	})
}

func execInPodWithPVC(kubeClient kubernetes.Interface, config *rest.Config, namespace string,
	pvcName string, command []string) (string, string, error) {
	pod, err := startPodWithPVCMount(kubeClient, namespace, pvcName)
	if err != nil {
		return "", "", err
	}

	defer func() {
		_ = ensurePodIsDeleted(kubeClient, namespace, pod.Name)
	}()

	log.WithField("namespace", namespace).
		WithField("pod", pod.Name).
		WithField("command", strings.Join(command, " ")).
		Info("Executing command in pod")
	return k8s.ExecInPod(kubeClient, config, namespace, pod.Name, command)
}

func startPodWithPVCMount(kubeClient kubernetes.Interface, namespace string, pvcName string) (*corev1.Pod, error) {
	terminationGracePeriodSeconds := int64(0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateTestResourceName(),
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   "docker.io/busybox:stable",
					Command: []string{"tail", "-f", "/dev/null"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "volume",
							MountPath: "/volume",
						},
					},
				},
			},
		},
	}

	log.WithFields(
		log.Fields{
			"namespace": pod.Namespace,
			"pod":       pod.Name,
			"pvc":       pvcName,
		}).Info("Creating pod with PVC mount")
	created, err := kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	err = wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		phase, err := getPodPhase(kubeClient, namespace, pod.Name)
		if err != nil {
			return false, err
		}

		running := *phase == corev1.PodRunning
		return running, nil
	})

	return created, err
}

func getPodPhase(kubeClient kubernetes.Interface, namespace string, name string) (*corev1.PodPhase, error) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &pod.Status.Phase, nil
}
