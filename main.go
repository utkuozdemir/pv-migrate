package main

import (
	"flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/kyokomi/emoji"
	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

var (
	sftpPodName = "pv-migrate-source-sftp-pod"
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
	target := flag.String("target", "", "Target persistent volume claim")
	targetNamespace := flag.String("target-namespace", "", "Target namespace")
	flag.Parse()

	if *source == "" || *sourceNamespace == "" || *target == "" || *targetNamespace == "" {
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

	sourceClaim, err := kubeClient.CoreV1().PersistentVolumeClaims(*sourceNamespace).Get(*source, v1.GetOptions{})
	if err != nil {
		panic("Failed to get source claim")
	}

	if sourceClaim.Status.Phase != corev1.ClaimBound {
		panic("Source claim not bound")
	}
	targetClaim, err := kubeClient.CoreV1().PersistentVolumeClaims(*targetNamespace).Get(*target, v1.GetOptions{})
	if err != nil {
		panic("Failed to get source claim")
	}

	if targetClaim.Status.Phase != corev1.ClaimBound {
		panic("Target claim not bound")
	}

	sourceOwnerNode, err := findOwnerNodeForPvc(kubeClient, sourceClaim)
	if err != nil {
		panic("Could not determine the owner of the source claim")
	}

	targetOwnerNode, err := findOwnerNodeForPvc(kubeClient, targetClaim)
	if err != nil {
		panic("Could not determine the owner of the target claim")
	}

	log.Info("Both claims exist and bound, proceeding...")
	if sourceClaim.Namespace != targetClaim.Namespace {
		log.Info("Case 1: Worst case - claims are in different namespaces. Let's see...")
		return
	} else {
		if sourceOwnerNode == targetOwnerNode {
			log.Info("Case 2: Lucky - claims are bound to the same node. This will be easy...")

			jobTtlSeconds := int32(600)
			jobName := "pv-migrate-" + randSeq(5)
			job := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: sourceClaim.Namespace,
				},
				Spec: batchv1.JobSpec{
					TTLSecondsAfterFinished: &jobTtlSeconds,
					Template: corev1.PodTemplateSpec{

						ObjectMeta: metav1.ObjectMeta{
							Name:      jobName,
							Namespace: sourceClaim.Namespace,
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "source-vol",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: sourceClaim.Name,
											ReadOnly:  true,
										},
									},
								}, {
									Name: "target-vol",
									VolumeSource: corev1.VolumeSource{
										PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
											ClaimName: targetClaim.Name,
											ReadOnly:  false,
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "docker.io/utkuozdemir/rsync:v0.1.0",
									Command: []string{
										"rsync", "-rtv", "/source/", "/target/",
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "source-vol",
											MountPath: "/source",
											ReadOnly:  true,
										},
										{
											Name:      "target-vol",
											MountPath: "/target",
											ReadOnly:  false,
										},
									},
								},
							},
							NodeName:      sourceOwnerNode,
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}

			jobsClient := kubeClient.BatchV1().Jobs(sourceClaim.Namespace)
			_ = jobsClient.Delete(job.Name, &metav1.DeleteOptions{})
			_, err := jobsClient.Create(&job)
			if err != nil {
				log.Panicf("Failed: %+v", err)
			}

			log.Infof("Created job: %s", jobName)

			// todo: do this blocking, tail the container logs etc: https://stackoverflow.com/a/32984298/1005102

		} else {
			if containsAnyAccessMode(sourceClaim.Status.AccessModes, corev1.ReadOnlyMany, corev1.ReadWriteMany) {
				log.Info("Case 3: We are fine, source claim can be read by many nodes...")
			} else if containsAnyAccessMode(targetClaim.Status.AccessModes, corev1.ReadWriteMany) {
				log.Info("Case 4: We are fine, target claim can be written by many nodes...")
			} else {
				log.Info("Case 5: Bad, claims are bound to different nodes and they can be bound to 1 node at once...")

				sourceSftpPod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      sftpPodName,
						Namespace: sourceClaim.Namespace,
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "source-vol",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: sourceClaim.Name,
										ReadOnly:  true,
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "rastasheep/ubuntu-sshd:18.04",
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "source-vol",
										MountPath: "/root/source",
										ReadOnly:  true,
									},
								},
							},
						},
						NodeName: sourceOwnerNode,
					},
				}

				podsClient := kubeClient.CoreV1().Pods(sourceClaim.Namespace)

				existingPod, getPodErr := podsClient.Get(sftpPodName, metav1.GetOptions{})
				if getPodErr != nil || existingPod.Name == "" {
					running := newPodRunningChannel(kubeClient, sourceClaim.Namespace, sftpPodName)
					_, err := podsClient.Create(&sourceSftpPod)
					if err != nil {
						panic(fmt.Sprintf("Failed: %+v", err))
					}
					<-*running
				} else {
					log.WithFields(log.Fields{
						"pod": sftpPodName,
					}).Info("Already exists")
				}

				log.WithFields(log.Fields{
					"pod": sftpPodName,
				}).Info(emoji.Sprint("Running :tada:"))
			}
		}
	}

}

func newPodDeletionChannel(kubeClient *kubernetes.Clientset, namespace string, podName string) *chan bool {
	deleted := make(chan bool)
	stopCh := make(chan struct{})
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
				pod := obj.(*corev1.Pod)
				if pod.Namespace == namespace && pod.Name == podName {
					close(stopCh)
					deleted <- true
				}
			},
		},
	)
	sharedInformerFactory.Start(stopCh)
	return &deleted
}

func newPodRunningChannel(kubeClient *kubernetes.Clientset, namespace string, podName string) *chan bool {
	running := make(chan bool)
	stopCh := make(chan struct{})
	defer close(stopCh)
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClient, 5*time.Second)
	sharedInformerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod := obj.(*corev1.Pod)
				if pod.Namespace == namespace && pod.Name == podName {
					if pod.Status.Phase == corev1.PodRunning {
						close(stopCh)
						running <- true
					}
				}
			},
			UpdateFunc: func(old interface{}, new interface{}) {
				newPod := new.(*corev1.Pod)
				if newPod.Namespace == namespace && newPod.Name == podName {
					if newPod.Status.Phase == corev1.PodRunning {
						close(stopCh)
						running <- true
					}
				}
			},
		},
	)
	sharedInformerFactory.Start(stopCh)
	return &running
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

func containsAnyAccessMode(sourceElements []corev1.PersistentVolumeAccessMode, elements ...corev1.PersistentVolumeAccessMode) bool {
	for _, sourceElement := range sourceElements {
		for _, element := range elements {
			if sourceElement == element {
				return true
			}
		}
	}
	return false
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
