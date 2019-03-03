package main

import (
	"flag"
	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

type claimInfo struct {
	ownerNode string
	claim     *corev1.PersistentVolumeClaim
}

func doCleanup(kubeClient *kubernetes.Clientset, instance string, namespace string) {
	log.WithFields(log.Fields{
		"instance":  instance,
		"namespace": namespace,
	}).Info("Doing cleanup")

	_ = kubeClient.BatchV1().Jobs(namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	_ = kubeClient.CoreV1().Pods(namespace).DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	serviceClient := kubeClient.CoreV1().Services(namespace)
	serviceList, _ := serviceClient.List(metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	for _, service := range serviceList.Items {
		_ = serviceClient.Delete(service.Name, &metav1.DeleteOptions{})
	}
	log.WithFields(log.Fields{
		"instance": instance,
	}).Info("Finished cleanup")
}

func main() {
	configureConsoleLogging()

	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	source := flag.String("source", "", "Source persistent volume claim")
	sourceNamespace := flag.String("source-namespace", "", "Source namespace")
	dest := flag.String("dest", "", "Destination persistent volume claim")
	destNamespace := flag.String("dest-namespace", "", "Destination namespace")
	flag.Parse()

	if *source == "" || *sourceNamespace == "" || *dest == "" || *destNamespace == "" {
		flag.Usage()
		return
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	sourceClaimInfo := buildClaimInfo(kubeClient, sourceNamespace, source)
	destClaimInfo := buildClaimInfo(kubeClient, destNamespace, dest)

	log.Info("Both claims exist and bound, proceeding...")
	instance := randSeq(5)

	handleSigterm(kubeClient, instance, *sourceNamespace, *destNamespace)

	defer doCleanup(kubeClient, instance, *sourceNamespace)
	defer doCleanup(kubeClient, instance, *destNamespace)

	migrateViaRsync(instance, kubeClient, sourceClaimInfo, destClaimInfo)
}

func handleSigterm(kubeClient *kubernetes.Clientset, instance string, sourceNamespace string, destNamespace string) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		doCleanup(kubeClient, instance, sourceNamespace)
		doCleanup(kubeClient, instance, destNamespace)
		os.Exit(1)
	}()
}

func prepareRsyncJob(instance string, destClaimInfo claimInfo, targetHost string) batchv1.Job {
	jobTtlSeconds := int32(600)
	backoffLimit := int32(0)
	jobName := "pv-migrate-rsync-" + instance
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: destClaimInfo.claim.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &jobTtlSeconds,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: destClaimInfo.claim.Namespace,
					Labels: map[string]string{
						"app":       "pv-migrate",
						"component": "rsync",
						"instance":  instance,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "dest-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: destClaimInfo.claim.Name,
									ReadOnly:  false,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           "docker.io/utkuozdemir/pv-migrate-rsync:v0.1.0",
							ImagePullPolicy: corev1.PullAlways,
							Command: []string{
								"rsync",
								"-avz",
								"-e",
								"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
								"root@" + targetHost + ":/source/",
								"/dest/",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
									ReadOnly:  false,
								},
							},
						},
					},
					NodeName:      destClaimInfo.ownerNode,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return job
}

func migrateViaRsync(instance string, kubeClient *kubernetes.Clientset, sourceClaimInfo claimInfo, destClaimInfo claimInfo) {
	sftpPod := prepareSshdPod(instance, sourceClaimInfo)
	createSshdPodWaitTillRunning(kubeClient, sftpPod)
	createdService := createSshdService(instance, kubeClient, sourceClaimInfo)
	targetHostName := createdService.Name + "." + createdService.Namespace + ".svc"
	rsyncJob := prepareRsyncJob(instance, destClaimInfo, targetHostName)
	createJobWaitTillCompleted(kubeClient, rsyncJob)
}

func prepareSshdPod(instance string, sourceClaimInfo claimInfo) corev1.Pod {
	podName := "pv-migrate-sshd-" + instance
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: sourceClaimInfo.claim.Namespace,
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
							ClaimName: sourceClaimInfo.claim.Name,
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
			NodeName: sourceClaimInfo.ownerNode,
		},
	}
}

func buildClaimInfo(kubeClient *kubernetes.Clientset, sourceNamespace *string, source *string) claimInfo {
	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(*sourceNamespace).Get(*source, v1.GetOptions{})
	if err != nil {
		log.Panic("Failed to get source claim")
	}
	if claim.Status.Phase != corev1.ClaimBound {
		log.Panic("Source claim not bound")
	}
	ownerNode, err := findOwnerNodeForPvc(kubeClient, claim)
	if err != nil {
		log.Panic("Could not determine the owner of the source claim")
	}
	return claimInfo{
		ownerNode: ownerNode,
		claim:     claim,
	}
}

func createSshdService(instance string, kubeClient *kubernetes.Clientset, sourceClaimInfo claimInfo) *corev1.Service {
	serviceName := "pv-migrate-sshd-" + instance
	createdService, err := kubeClient.CoreV1().Services(sourceClaimInfo.claim.Namespace).Create(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: sourceClaimInfo.claim.Namespace,
				Labels: map[string]string{
					"app":       "pv-migrate",
					"component": "sshd",
					"instance":  instance,
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
					"app":       "pv-migrate",
					"component": "sshd",
					"instance":  instance,
				},
			},
		},
	)
	if err != nil {
		log.Panicf("Failed: %+v", err)
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
	createdPod, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(&pod)
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

func createJobWaitTillCompleted(kubeClient *kubernetes.Clientset, job batchv1.Job) {
	succeeded := make(chan bool)
	defer close(succeeded)
	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == job.Namespace && newPod.Labels["job-name"] == job.Name {
					switch newPod.Status.Phase {
					case corev1.PodSucceeded:
						log.WithFields(log.Fields{
							"jobName": job.Name,
							"podName": newPod.Name,
						}).Info("Job completed...")
						succeeded <- true
					case corev1.PodRunning:
						log.WithFields(log.Fields{
							"jobName": job.Name,
							"podName": newPod.Name,
						}).Info("Job is running ")
					case corev1.PodFailed, corev1.PodUnknown:
						log.WithFields(log.Fields{
							"jobName": job.Name,
							"podName": newPod.Name,
						}).Panic("Job failed, exiting")
					}
				}
			},
		},
	)

	sharedInformerFactory.Start(stopCh)

	log.WithFields(log.Fields{
		"jobName": job.Name,
	}).Info("Creating rsync job")
	_, err := kubeClient.BatchV1().Jobs(job.Namespace).Create(&job)
	if err != nil {
		log.WithFields(log.Fields{
			"jobName": job.Name,
		}).Panic("Failed to create rsync job")
	}

	log.WithFields(log.Fields{
		"jobName": job.Name,
	}).Info("Waiting for rsync job to finish")
	<-succeeded
}

func findOwnerNodeForPvc(kubeClient *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) (string, error) {
	podList, err := kubeClient.CoreV1().Pods(pvc.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, pod := range podList.Items {
		for _, volume := range pod.Spec.Volumes {
			persistentVolumeClaim := volume.PersistentVolumeClaim
			if persistentVolumeClaim != nil && persistentVolumeClaim.ClaimName == pvc.Name {
				return pod.Spec.NodeName, nil
			}
		}
	}

	return "", nil
}

func configureConsoleLogging() {
	if err := flag.Set("logtostderr", "true"); err != nil {
		glog.Errorf("Failed to set logging to stderr: %v", err)
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
