package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/k8s"
)

type Info struct {
	ClusterClient      *k8s.ClusterClient
	Claim              *corev1.PersistentVolumeClaim
	MountedNode        string
	AffinityHelmValues map[string]any
	SupportsRWO        bool
	SupportsROX        bool
	SupportsRWX        bool
}

//nolint:cyclop
func New(ctx context.Context, client *k8s.ClusterClient, namespace string, name string) (*Info, error) {
	kubeClient := client.KubeClient

	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pvc %s/%s: %w", namespace, name, err)
	}

	supportsRWO := false
	supportsROX := false
	supportsRWX := false

	readWriteOncePod := false

	for _, accessMode := range claim.Spec.AccessModes {
		switch accessMode {
		case corev1.ReadWriteOncePod:
			supportsRWO = true
			readWriteOncePod = true
		case corev1.ReadWriteOnce:
			supportsRWO = true
		case corev1.ReadOnlyMany:
			supportsROX = true
		case corev1.ReadWriteMany:
			supportsRWX = true
		}
	}

	mountedNode, err := findMountedNode(ctx, kubeClient, claim)
	if err != nil {
		return nil, err
	}

	if readWriteOncePod && mountedNode != "" {
		return nil, fmt.Errorf("pvc %s/%s is mounted to a pod and has ReadWriteOncePod "+
			"access mode, it cannot be mounted to the migration pod", namespace, name)
	}

	required := !supportsRWX && !supportsROX

	affinityHelmValues := buildAffinityHelmValues(mountedNode, required)

	return &Info{
		ClusterClient:      client,
		Claim:              claim,
		MountedNode:        mountedNode,
		AffinityHelmValues: affinityHelmValues,
		SupportsRWO:        supportsRWO,
		SupportsROX:        supportsROX,
		SupportsRWX:        supportsRWX,
	}, nil
}

func findMountedNode(ctx context.Context, kubeClient kubernetes.Interface,
	pvc *corev1.PersistentVolumeClaim,
) (string, error) {
	podList, err := kubeClient.CoreV1().Pods(pvc.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
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

func buildAffinityHelmValues(nodeName string, required bool) map[string]any {
	if nodeName == "" {
		return nil
	}

	terms := map[string]any{
		"matchFields": []map[string]any{
			{
				"key":      "metadata.name",
				"operator": "In",
				"values":   []string{nodeName},
			},
		},
	}

	if required {
		return map[string]any{
			"nodeAffinity": map[string]any{
				"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
					"nodeSelectorTerms": []map[string]any{terms},
				},
			},
		}
	}

	return map[string]any{
		"nodeAffinity": map[string]any{
			"preferredDuringSchedulingIgnoredDuringExecution": []map[string]any{
				{
					"weight":     100,
					"preference": terms,
				},
			},
		},
	}
}
