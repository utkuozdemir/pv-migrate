// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//nolint:funlen
package pvc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("should have required affinity when only RWO is supported", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOnce)

		pvcInfo, err := pvc.New(ctx, clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Equal(t, clusterClient, pvcInfo.ClusterClient)
		assert.Equal(t, "test", pvcInfo.Claim.Name)
		assert.Equal(t, "testns", pvcInfo.Claim.Namespace)
		assert.Equal(t, "node-2", pvcInfo.MountedNode)
		assert.True(t, pvcInfo.SupportsRWO)
		assert.False(t, pvcInfo.SupportsROX)
		assert.False(t, pvcInfo.SupportsRWX)
		assert.Equal(t, map[string]any{
			"nodeAffinity": map[string]any{
				"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
					"nodeSelectorTerms": []map[string]any{
						{
							"matchFields": []map[string]any{
								{
									"key":      "metadata.name",
									"operator": "In",
									"values":   []string{"node-2"},
								},
							},
						},
					},
				},
			},
		}, pvcInfo.AffinityHelmValues)
	})

	t.Run("should have preferred affinity if it supports ROX", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOnce, corev1.ReadOnlyMany)

		pvcInfo, err := pvc.New(ctx, clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Equal(t, clusterClient, pvcInfo.ClusterClient)
		assert.Equal(t, "test", pvcInfo.Claim.Name)
		assert.Equal(t, "testns", pvcInfo.Claim.Namespace)
		assert.Equal(t, "node-2", pvcInfo.MountedNode)
		assert.True(t, pvcInfo.SupportsRWO)
		assert.True(t, pvcInfo.SupportsROX)
		assert.False(t, pvcInfo.SupportsRWX)
		assert.Equal(t, map[string]any{
			"nodeAffinity": map[string]any{
				"preferredDuringSchedulingIgnoredDuringExecution": []map[string]any{
					{
						"weight": 100,
						"preference": map[string]any{
							"matchFields": []map[string]any{
								{
									"key":      "metadata.name",
									"operator": "In",
									"values":   []string{"node-2"},
								},
							},
						},
					},
				},
			},
		}, pvcInfo.AffinityHelmValues)
	})

	t.Run("ReadWriteOncePod with mounting pod is not supported", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOncePod)

		_, err := pvc.New(ctx, clusterClient, "testns", "test")
		require.ErrorContains(t, err, "ReadWriteOncePod")
	})

	t.Run("ReadWriteOncePod with no mounting pod is supported", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		clusterClient := buildClusterClient("", corev1.ReadWriteOncePod)

		pvcInfo, err := pvc.New(ctx, clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Empty(t, pvcInfo.MountedNode)
	})
}

func TestSize(t *testing.T) {
	t.Parallel()

	t.Run("prefers actual capacity over requested", func(t *testing.T) {
		t.Parallel()

		info := &pvc.Info{Claim: &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				},
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")},
			},
		}}

		size := info.Size()
		assert.Equal(t, "2Gi", size.String())
	})

	t.Run("falls back to requested when not bound", func(t *testing.T) {
		t.Parallel()

		info := &pvc.Info{Claim: &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				},
			},
		}}

		size := info.Size()
		assert.Equal(t, "1Gi", size.String())
	})

	t.Run("returns zero when nothing is set", func(t *testing.T) {
		t.Parallel()

		info := &pvc.Info{Claim: &corev1.PersistentVolumeClaim{}}

		size := info.Size()
		assert.True(t, size.IsZero())
	})
}

func buildClusterClient(
	mountingNode string,
	pvcAccessModes ...corev1.PersistentVolumeAccessMode,
) *k8s.ClusterClient {
	testPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: pvcAccessModes,
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "testns",
		},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Volumes: []corev1.Volume{
				{
					Name: "something",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "something",
						},
					},
				},
			},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "testns",
		},
		Spec: corev1.PodSpec{
			NodeName: mountingNode,
			Volumes: []corev1.Volume{
				{
					Name: "something-else",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "something-else",
						},
					},
				},
			},
		},
	}

	if mountingNode != "" {
		pod2.Spec.Volumes = append(pod2.Spec.Volumes, corev1.Volume{
			Name: "test",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "test",
				},
			},
		})
	}

	objects := []runtime.Object{
		testPVC,
		pod1,
		pod2,
	}
	kubeClient := fake.NewClientset(objects...)

	return &k8s.ClusterClient{
		KubeClient: kubeClient,
	}
}
