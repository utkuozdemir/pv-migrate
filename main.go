package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	// needed for k8s oidc and gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
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
	ownerNode             string
	claim                 *corev1.PersistentVolumeClaim
	readOnly              bool
	svcType               corev1.ServiceType
	deleteExtraneousFiles bool
}

func doCleanup(kubeClient *kubernetes.Clientset, instance string, namespace string) {
	log.WithFields(log.Fields{
		"instance":  instance,
		"namespace": namespace,
	}).Info("Doing cleanup")

	_ = kubeClient.BatchV1().Jobs(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	_ = kubeClient.CoreV1().Pods(namespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	serviceClient := kubeClient.CoreV1().Services(namespace)
	serviceList, _ := serviceClient.List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=pv-migrate,instance=" + instance,
	})

	for _, service := range serviceList.Items {
		_ = serviceClient.Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
	}
	log.WithFields(log.Fields{
		"instance": instance,
	}).Info("Finished cleanup")
}

func buildConfigFromFlags(context, kubeconfigPath string) (*rest.Config, error) {
	clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		clientConfigLoadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	source := flag.String("source", "", "Source persistent volume claim")
	sourceNamespace := flag.String("source-namespace", "", "Source namespace")
	sourceContext := flag.String("source-context", "", "(optional) Source context")
	dest := flag.String("dest", "", "Destination persistent volume claim")
	destNamespace := flag.String("dest-namespace", "", "Destination namespace")
	destContext := flag.String("dest-context", "", "(optional) Destination context")
	sourceReadOnly := flag.Bool("sourceReadOnly", true, "(optional) source pvc ReadOnly")
	deleteExtraneousFromDest := flag.Bool("dest-delete-extraneous-files", false, "(optional) delete extraneous files from destination dirs")
	maxRetriesFetchServiceIP := flag.Int("max-retries-fetch-service-ip", 30, "(optional) maximum retries to fetch ip from service, retries * 10 seconds")
	flag.Parse()

	if *deleteExtraneousFromDest {
		log.Warn("delete extraneous files from dest is enabled")
	}

	if *source == "" || *sourceNamespace == "" || *dest == "" || *destNamespace == "" {
		flag.Usage()
		return
	}

	svcType := corev1.ServiceTypeClusterIP
	if *sourceContext != *destContext {
		svcType = corev1.ServiceTypeLoadBalancer
	}

	sourceCfg, err := buildConfigFromFlags(*sourceContext, *kubeconfig)
	if err != nil {
		log.WithError(err).Fatal("Error building kubeconfig")
	}

	sourceKubeClient, err := kubernetes.NewForConfig(sourceCfg)
	if err != nil {
		log.WithError(err).Fatal("Error building kubernetes clientset")
	}

	destCfg, err := buildConfigFromFlags(*destContext, *kubeconfig)
	if err != nil {
		log.WithError(err).Fatal("Error building kubeconfig")
	}

	destKubeClient, err := kubernetes.NewForConfig(destCfg)
	if err != nil {
		log.WithError(err).Fatal("Error building kubernetes clientset")
	}

	sourceClaimInfo := buildClaimInfo(sourceKubeClient, sourceNamespace, source, *sourceReadOnly, false, svcType)
	destClaimInfo := buildClaimInfo(destKubeClient, destNamespace, dest, false, *deleteExtraneousFromDest, svcType)

	log.Info("Both claims exist and bound, proceeding...")
	instance := randSeq(5)

	handleSigterm(sourceKubeClient, destKubeClient, instance, *sourceNamespace, *destNamespace)

	defer doCleanup(sourceKubeClient, instance, *sourceNamespace)
	defer doCleanup(destKubeClient, instance, *destNamespace)

	migrateViaRsync(instance, sourceKubeClient, destKubeClient, sourceClaimInfo, destClaimInfo, maxRetriesFetchServiceIP)
}

func handleSigterm(sourceKubeClient, destKubeClient *kubernetes.Clientset, instance string, sourceNamespace string, destNamespace string) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		doCleanup(sourceKubeClient, instance, sourceNamespace)
		doCleanup(destKubeClient, instance, destNamespace)
		os.Exit(1)
	}()
}

func buildRsyncCommand(claimInfo claimInfo, targetHost string) []string {
	rsyncCommand := []string{"rsync"}
	if claimInfo.deleteExtraneousFiles {
		rsyncCommand = append(rsyncCommand, "--delete")
	}
	rsyncCommand = append(rsyncCommand, "-avz")
	rsyncCommand = append(rsyncCommand, "-e")
	rsyncCommand = append(rsyncCommand, "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null")
	rsyncCommand = append(rsyncCommand, fmt.Sprintf("root@%s:/source/", targetHost))
	rsyncCommand = append(rsyncCommand, "/dest/")

	return rsyncCommand
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
									ReadOnly:  destClaimInfo.readOnly,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "app",
							Image:           "docker.io/utkuozdemir/pv-migrate-rsync:v0.1.0",
							ImagePullPolicy: corev1.PullAlways,
							Command:         buildRsyncCommand(destClaimInfo, targetHost),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dest-vol",
									MountPath: "/dest",
									ReadOnly:  destClaimInfo.readOnly,
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

func migrateViaRsync(instance string, sourcekubeClient *kubernetes.Clientset, destkubeClient *kubernetes.Clientset, sourceClaimInfo claimInfo, destClaimInfo claimInfo, maxRetriesFetchServiceIP int) {
	sftpPod := prepareSshdPod(instance, sourceClaimInfo)
	createSshdPodWaitTillRunning(sourcekubeClient, sftpPod)
	createdService := createSshdService(instance, sourcekubeClient, sourceClaimInfo)
	targetServiceAddress := getServiceAddress(createdService, sourcekubeClient, maxRetriesFetchServiceIP)

	if targetServiceAddress == "" {
		return
	}

	log.Infof("use service address %s to connect to rsync server", targetServiceAddress)
	rsyncJob := prepareRsyncJob(instance, destClaimInfo, targetServiceAddress)
	createJobWaitTillCompleted(destkubeClient, rsyncJob)
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
							ReadOnly:  sourceClaimInfo.readOnly,
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
							ReadOnly:  sourceClaimInfo.readOnly,
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

func buildClaimInfo(kubeClient *kubernetes.Clientset, sourceNamespace *string, source *string, readOnly, deleteExtraneousFiles bool, svcType corev1.ServiceType) claimInfo {
	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(*sourceNamespace).Get(context.TODO(), *source, v1.GetOptions{})
	if err != nil {
		log.WithError(err).WithField("pvc", *source).Fatal("Failed to get source claim")
	}
	if claim.Status.Phase != corev1.ClaimBound {
		log.Fatal("Source claim not bound")
	}
	ownerNode, err := findOwnerNodeForPvc(kubeClient, claim)
	if err != nil {
		log.Fatal("Could not determine the owner of the source claim")
	}
	return claimInfo{
		ownerNode:             ownerNode,
		claim:                 claim,
		readOnly:              readOnly,
		svcType:               svcType,
		deleteExtraneousFiles: deleteExtraneousFiles,
	}
}

func getServiceAddress(svc *corev1.Service, kubeClient *kubernetes.Clientset, maxRetries int) string {
	if svc.Spec.Type == corev1.ServiceTypeClusterIP {
		return svc.Spec.ClusterIP
	}

	sleepInterval := 10 * time.Second
	retryCounter := 0

	for {
		if retryCounter > maxRetries {
			log.Error("unable to get external ip from svc, maximum retries reached")
			return ""
		}

		retryCounter++

		createdService, err := kubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, v1.GetOptions{})
		if err != nil {
			log.Fatal("unable to get service")
		}

		if len(createdService.Status.LoadBalancer.Ingress) == 0 {
			log.WithField("retry", retryCounter).Infof("wait for external ip, sleep %s", sleepInterval)
			time.Sleep(sleepInterval)
			continue
		}
		return createdService.Status.LoadBalancer.Ingress[0].IP
	}
}

func createSshdService(instance string, kubeClient *kubernetes.Clientset, sourceClaimInfo claimInfo) *corev1.Service {
	serviceName := "pv-migrate-sshd-" + instance
	createdService, err := kubeClient.CoreV1().Services(sourceClaimInfo.claim.Namespace).Create(
		context.TODO(),
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
				Type: sourceClaimInfo.svcType,
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
	_, err := kubeClient.BatchV1().Jobs(job.Namespace).Create(context.TODO(), &job, metav1.CreateOptions{})
	if err != nil {
		log.WithFields(log.Fields{
			"jobName": job.Name,
		}).WithError(err).Fatal("Failed to create rsync job")
	}

	log.WithFields(log.Fields{
		"jobName": job.Name,
	}).Info("Waiting for rsync job to finish")
	<-succeeded
}

func findOwnerNodeForPvc(kubeClient *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) (string, error) {
	podList, err := kubeClient.CoreV1().Pods(pvc.Namespace).List(context.TODO(), metav1.ListOptions{})
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
