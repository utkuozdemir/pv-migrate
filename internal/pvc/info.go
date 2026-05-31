package pvc

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
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

//
//nolint:cyclop
func New(
	ctx context.Context,
	client *k8s.ClusterClient,
	ns, name string,
) (*Info, error) {
	kubeClient := client.KubeClient

	claim, err := kubeClient.CoreV1().PersistentVolumeClaims(ns).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pvc %s/%s: %w", ns, name, err)
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
			"access mode, it cannot be mounted to the migration pod", ns, name)
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

// Size returns the storage capacity of the PVC. It prefers the actual capacity
// of the bound PersistentVolume (Status.Capacity) and falls back to the
// requested capacity (Spec.Resources.Requests) when the PVC is not yet bound.
// It returns a zero quantity when neither is set.
func (i *Info) Size() resource.Quantity {
	if i == nil || i.Claim == nil {
		return resource.Quantity{}
	}

	if capacity, ok := i.Claim.Status.Capacity[corev1.ResourceStorage]; ok && !capacity.IsZero() {
		return capacity
	}

	return i.Claim.Spec.Resources.Requests[corev1.ResourceStorage]
}

const (
	// storageProvisionerAnnotation is set on a PVC by the controller to record
	// which provisioner handles its dynamic provisioning.
	storageProvisionerAnnotation = "volume.kubernetes.io/storage-provisioner"
	// betaStorageProvisionerAnnotation is the pre-1.23 form of the above; some
	// clusters still carry it.
	betaStorageProvisionerAnnotation = "volume.beta.kubernetes.io/storage-provisioner"
)

// Provisioner resolves the name of the storage provisioner backing the PVC on a
// best-effort basis. It first reads the provisioner annotation set on the claim,
// which is present once a dynamically provisioned PVC has been picked up by the
// controller, then falls back to the provisioner of the claim's StorageClass.
// The fallback is needed for PVCs that are not yet bound (e.g. those using the
// WaitForFirstConsumer volume binding mode), whose annotation is not set yet.
// It returns an empty name when the provisioner cannot be determined; the
// returned error is non-nil only when a StorageClass lookup was attempted and
// failed, so callers may treat it as best-effort.
func (i *Info) Provisioner(ctx context.Context) (string, error) {
	if i == nil || i.Claim == nil {
		return "", nil
	}

	if provisioner := i.Claim.Annotations[storageProvisionerAnnotation]; provisioner != "" {
		return provisioner, nil
	}

	if provisioner := i.Claim.Annotations[betaStorageProvisionerAnnotation]; provisioner != "" {
		return provisioner, nil
	}

	storageClassName := i.Claim.Spec.StorageClassName
	if storageClassName == nil || *storageClassName == "" {
		return "", nil
	}

	if i.ClusterClient == nil || i.ClusterClient.KubeClient == nil {
		return "", nil
	}

	storageClass, err := i.ClusterClient.KubeClient.StorageV1().
		StorageClasses().Get(ctx, *storageClassName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get storage class %s: %w", *storageClassName, err)
	}

	return storageClass.Provisioner, nil
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
					"weight":     100, //nolint:mnd
					"preference": terms,
				},
			},
		},
	}
}
