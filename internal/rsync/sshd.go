package rsync

import (
	"context"
	log "github.com/sirupsen/logrus"
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

func createSshdService(instance string, kubeClient *kubernetes.Clientset, sourceClaimInfo k8s.ClaimInfo) *corev1.Service {
	serviceName := "pv-migrate-sshd-" + instance
	createdService, err := kubeClient.CoreV1().Services(sourceClaimInfo.Claim.Namespace).Create(
		context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: sourceClaimInfo.Claim.Namespace,
				Labels: map[string]string{
					"app":       "pv-migrate",
					"component": "sshd",
					"instance":  instance,
				},
			},
			Spec: corev1.ServiceSpec{
				Type: sourceClaimInfo.SvcType,
				Ports: []corev1.ServicePort{
					{
						Port:       22,
						TargetPort: intstr.FromInt(22),
					},
				},
				Selector: map[string]string{
					"app":       "pv-migrate",
					"component": "sshd",
					"instance":  instance,
				},
			},
		},
		v1.CreateOptions{},
	)
	if err != nil {
		log.WithError(err).Fatal("service creation failed")
	}
	return createdService
}

func createSshdPodWaitTillRunning(kubeClient *kubernetes.Clientset, pod corev1.Pod) *corev1.Pod {
	running := make(chan bool)
	defer close(running)
	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == pod.Namespace && newPod.Name == pod.Name {
					switch newPod.Status.Phase {
					case corev1.PodRunning:
						log.WithFields(log.Fields{
							"podName": pod.Name,
						}).Info("sshd pod running")
						running <- true

					case corev1.PodFailed, corev1.PodUnknown:
						log.WithFields(log.Fields{
							"podName": newPod.Name,
						}).Panic("sshd pod failed to start, exiting")
					}
				}
			},
		},
	)
	sharedInformerFactory.Start(stopCh)

	log.WithFields(log.Fields{
		"podName": pod.Name,
	}).Info("Creating sshd pod")
	createdPod, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		log.WithFields(log.Fields{
			"podName": pod.Name,
		}).Panic("Failed to create sshd pod")
	}

	log.WithFields(log.Fields{
		"podName": pod.Name,
	}).Info("Waiting for pod to start running")
	<-running

	return createdPod
}

func prepareSshdPod(instance string, sourceClaimInfo k8s.ClaimInfo) corev1.Pod {
	podName := "pv-migrate-sshd-" + instance
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: sourceClaimInfo.Claim.Namespace,
			Labels: map[string]string{
				"app":       "pv-migrate",
				"component": "sshd",
				"instance":  instance,
			},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "source-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: sourceClaimInfo.Claim.Name,
							ReadOnly:  sourceClaimInfo.ReadOnly,
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
							ReadOnly:  sourceClaimInfo.ReadOnly,
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 22,
						},
					},
				},
			},
			NodeName: sourceClaimInfo.OwnerNode,
		},
	}
}
