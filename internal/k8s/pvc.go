package k8s

import (
	"context"
	"errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PvcInfo struct {
	KubeClient  *kubernetes.Clientset
	Claim       *corev1.PersistentVolumeClaim
	MountedNode string
}

func BuildPvcInfo(kubeClient *kubernetes.Clientset, namespace string, name string) (*PvcInfo, error) {
	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, v1.GetOptions{})
	if err != nil {
		return nil, err
	}

	mountedNode, err := findMountedNodeForPvc(kubeClient, claim)
	if err != nil {
		return nil, err
	}

	return &PvcInfo{
		KubeClient:  kubeClient,
		Claim:       claim,
		MountedNode: mountedNode,
	}, nil
}

func findMountedNodeForPvc(kubeClient *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) (string, error) {
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

	return "", errors.New("couldn't find the node that the pvc is mounted to")
}
