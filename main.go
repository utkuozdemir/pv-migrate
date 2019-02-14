package main

import (
	"flag"
	"fmt"
	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"path/filepath"
)

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

	fmt.Println("Both claims exist and bound, proceeding...")
	if sourceClaim.Namespace != targetClaim.Namespace {
		fmt.Println("Case 1: Worst case - claims are in different namespaces. Let's see...")
		return
	} else {
		if sourceOwnerNode == targetOwnerNode {
			fmt.Println("Case 2: Lucky - claims are bound to the same node. This will be easy...")

			jobTtlSeconds := int32(600)
			job := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pv-migrate-temp",
					Namespace: sourceClaim.Namespace,
				},
				Spec: batchv1.JobSpec{
					TTLSecondsAfterFinished: &jobTtlSeconds,
					Template: corev1.PodTemplateSpec{

						ObjectMeta: metav1.ObjectMeta{
							Name:      "pv-migrate-temp",
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

			//pod := corev1.Pod{}
			//createdPod, err := kubeClient.CoreV1().Pods(sourceClaim.Namespace).Create(&pod)
			jobsClient := kubeClient.BatchV1().Jobs(sourceClaim.Namespace)

			_ = jobsClient.Delete(job.Name, &metav1.DeleteOptions{})

			createdJob, err := jobsClient.Create(&job)
			if err != nil {
				panic(fmt.Sprintf("Failed: %+v", err))
			}

			//fmt.Printf("Created pod: %+v\n", createdPod)
			fmt.Printf("Created job: %+v\n", createdJob)

			// todo: do this blocking, tail the container logs etc: https://stackoverflow.com/a/32984298/1005102

		} else {
			if containsAnyAccessMode(sourceClaim.Status.AccessModes, corev1.ReadOnlyMany, corev1.ReadWriteMany) {
				fmt.Println("Case 3: We are fine, source claim can be read by many nodes...")
			} else if containsAnyAccessMode(targetClaim.Status.AccessModes, corev1.ReadWriteMany) {
				fmt.Println("Case 4: We are fine, target claim can be written by many nodes...")
			} else {
				fmt.Println("Case 5: Bad, claims are bound to different nodes and they can be bound to 1 node at once...")
			}
		}
	}

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
