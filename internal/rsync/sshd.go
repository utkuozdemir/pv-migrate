package rsync

import (
	"context"
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/constants"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"time"
)

func CreateSshdService(instance string, sourcePvcInfo k8s.PvcInfo) (*corev1.Service, error) {
	kubeClient := sourcePvcInfo.KubeClient()
	serviceName := "pv-migrate-sshd-" + instance
	createdService, err := kubeClient.CoreV1().Services(sourcePvcInfo.Claim().Namespace).Create(
		context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: sourcePvcInfo.Claim().Namespace,
				Labels: map[string]string{
					constants.AppLabelKey:      constants.AppLabelValue,
					constants.InstanceLabelKey: instance,
					"component":                "sshd",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port:       22,
						TargetPort: intstr.FromInt(22),
					},
				},
				Selector: map[string]string{
					constants.AppLabelKey:      constants.AppLabelValue,
					constants.InstanceLabelKey: instance,
					"component":                "sshd",
				},
			},
		},
		v1.CreateOptions{},
	)
	if err != nil {
		return nil, err
	}
	return createdService, nil
}

func CreateSshdPodWaitTillRunning(kubeClient kubernetes.Interface, pod *corev1.Pod) error {
	running := make(chan bool)
	defer close(running)
	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	logger := log.WithField("podName", pod.Name)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == pod.Namespace && newPod.Name == pod.Name {
					switch newPod.Status.Phase {
					case corev1.PodRunning:
						logger.Info("Sshd pod running")
						running <- true

					case corev1.PodFailed, corev1.PodUnknown:
						logger.Error("Sshd pod failed")
						running <- false
					}
				}
			},
		},
	)
	sharedInformerFactory.Start(stopCh)

	logger.Info("Creating sshd pod")
	_, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	logger.Info("Waiting for the sshd pod to start running")
	if !<-running {
		return errors.New("sshd pod failed to start")
	}

	return nil
}

func PrepareSshdPod(instance string, sourcePvcInfo k8s.PvcInfo) *corev1.Pod {
	podName := "pv-migrate-sshd-" + instance
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: sourcePvcInfo.Claim().Namespace,
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: instance,
				"component":                "sshd",
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "source-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: sourcePvcInfo.Claim().Name,
							ReadOnly:  true,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "app",
					Image:           "docker.io/utkuozdemir/pv-migrate-sshd:v0.1.0",
					ImagePullPolicy: corev1.PullAlways,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "source-vol",
							MountPath: "/source",
							ReadOnly:  true,
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 22,
						},
					},
				},
			},
			NodeName: sourcePvcInfo.MountedNode(),
		},
	}
}
