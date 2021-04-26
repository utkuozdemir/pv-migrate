package integrationtest

import (
	"context"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultPVCCapacity = "64Mi"
)

func ensurePVCExistsAndBound(kubeClient kubernetes.Interface, namespace string,
	name string) (*corev1.PersistentVolumeClaim, error) {
	foundPVC, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err == nil {
		return foundPVC, err
	}

	if !kubeerrors.IsNotFound(err) {
		return nil, err
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse(defaultPVCCapacity),
				},
			},
		},
	}

	createdPVC, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	err = ensurePVCIsBound(kubeClient, namespace, name)
	if err != nil {
		return nil, err
	}

	return createdPVC, nil
}

func ensurePVCIsBound(kubeClient kubernetes.Interface, namespace string, name string) error {
	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil
	}

	if pvc.Status.Phase == corev1.ClaimBound {
		return nil
	}

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
							ClaimName: name,
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

	podLogger := log.WithField("namespace", pod.Namespace).WithField("pod", pod.Name)
	podLogger.Info("Creating binder pod")
	_, err = kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	defer func() {
		_ = ensurePodIsDeleted(kubeClient, pod.Namespace, pod.Name)
	}()

	return waitUntilPVCIsBound(kubeClient, namespace, name)
}

func waitUntilPVCIsBound(kubeClient kubernetes.Interface, namespace string, name string) error {
	logger := log.WithField("namespace", namespace).WithField("name", name)
	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		phase, err := getPVCPhase(kubeClient, namespace, name)
		if err != nil {
			return false, err
		}

		done = *phase == corev1.ClaimBound
		logger.WithField("phase", phase).Info("Still not bound, polling...")
		return done, err
	})
}

func getPVCPhase(kubeClient kubernetes.Interface, namespace string, name string) (*corev1.PersistentVolumeClaimPhase, error) {
	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &pvc.Status.Phase, nil
}
