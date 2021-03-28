package k8s

import (
	"context"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ClaimInfo struct {
	OwnerNode             string
	Claim                 *corev1.PersistentVolumeClaim
	ReadOnly              bool
	SvcType               corev1.ServiceType
	DeleteExtraneousFiles bool
}

func FindOwnerNodeForPvc(kubeClient *kubernetes.Clientset, pvc *corev1.PersistentVolumeClaim) (string, error) {
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

func BuildClaimInfo(kubeClient *kubernetes.Clientset, sourceNamespace *string, source *string, readOnly, deleteExtraneousFiles bool, svcType corev1.ServiceType) ClaimInfo {
	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(*sourceNamespace).Get(context.TODO(), *source, v1.GetOptions{})
	if err != nil {
		log.WithError(err).WithField("pvc", *source).Fatal("Failed to get source claim")
	}
	if claim.Status.Phase != corev1.ClaimBound {
		log.Fatal("Source claim not bound")
	}
	ownerNode, err := FindOwnerNodeForPvc(kubeClient, claim)
	if err != nil {
		log.Fatal("Could not determine the owner of the source claim")
	}
	return ClaimInfo{
		OwnerNode:             ownerNode,
		Claim:                 claim,
		ReadOnly:              readOnly,
		SvcType:               svcType,
		DeleteExtraneousFiles: deleteExtraneousFiles,
	}
}
