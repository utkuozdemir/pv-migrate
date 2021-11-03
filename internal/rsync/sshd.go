package rsync

import (
	"context"
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"time"
)

func CreateSshdService(instanceId string, sourcePvcInfo *pvc.Info, serviceType corev1.ServiceType) (*corev1.Service, error) {
	kubeClient := sourcePvcInfo.KubeClient
	serviceName := "pv-migrate-sshd-" + instanceId
	createdService, err := kubeClient.CoreV1().Services(sourcePvcInfo.Claim.Namespace).Create(
		context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: sourcePvcInfo.Claim.Namespace,
				Labels:    k8s.ComponentLabels(instanceId, k8s.Sshd),
			},
			Spec: corev1.ServiceSpec{
				Type: serviceType,
				Ports: []corev1.ServicePort{
					{
						Port:       22,
						TargetPort: intstr.FromInt(22),
					},
				},
				Selector: k8s.ComponentLabels(instanceId, k8s.Sshd),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, err
	}
	return createdService, nil
}

func CreateSshdPodWaitTillRunning(logger *log.Entry, kubeClient kubernetes.Interface, pod *corev1.Pod) error {
	running := make(chan bool)
	defer close(running)
	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	logger = logger.WithField("pod", pod.Name)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == pod.Namespace && newPod.Name == pod.Name {
					switch newPod.Status.Phase {
					case corev1.PodRunning:
						logger.Info(":rocket: Sshd pod started")
						running <- true

					case corev1.PodFailed:
						logger.Error(":cross_mark: Sshd pod failed")
						running <- false
					}
				}
			},
		},
	)
	sharedInformerFactory.Start(stopCh)

	logger.Info(":rocket: Creating sshd pod")
	_, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	logger.Info(":hourglass_not_done: Waiting for the sshd pod to start running")
	if !<-running {
		return errors.New("sshd pod failed to start")
	}

	return nil
}

func createSshdPublicKeySecret(instanceId string, sourcePvcInfo *pvc.Info, publicKey string) (*corev1.Secret, error) {
	kubeClient := sourcePvcInfo.KubeClient
	namespace := sourcePvcInfo.Claim.Namespace
	name := "pv-migrate-sshd-" + instanceId
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    k8s.ComponentLabels(instanceId, k8s.Sshd),
		},
		Data: map[string][]byte{
			"publicKey": []byte(publicKey),
		},
	}

	secrets := kubeClient.CoreV1().Secrets(namespace)
	return secrets.Create(context.TODO(), &secret, metav1.CreateOptions{})
}

func PrepareSshdPod(instanceId string, sourcePvcInfo *pvc.Info, publicKeySecretName string,
	sshdImage string, sshdServiceAccount string, mountReadOnly bool) *corev1.Pod {
	podName := "pv-migrate-sshd-" + instanceId
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: sourcePvcInfo.Claim.Namespace,
			Labels:    k8s.ComponentLabels(instanceId, k8s.Sshd),
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "source-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: sourcePvcInfo.Claim.Name,
							ReadOnly:  mountReadOnly,
						},
					},
				},
				{
					Name: "public-key-vol",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: publicKeySecretName,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: sshdImage,
					Env: []corev1.EnvVar{
						{
							Name:  "SSH_ENABLE_ROOT",
							Value: "true",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "source-vol",
							MountPath: "/source",
							ReadOnly:  mountReadOnly,
						},
						{
							Name:      "public-key-vol",
							MountPath: "/root/.ssh/authorized_keys",
							SubPath:   "publicKey",
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 22,
						},
					},
				},
			},
			NodeName:           sourcePvcInfo.MountedNode,
			ServiceAccountName: sshdServiceAccount,
		},
	}
}
