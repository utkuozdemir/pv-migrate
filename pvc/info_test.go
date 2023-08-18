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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/pvc"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("should have required affinity when only RWO is supported", func(t *testing.T) {
		t.Parallel()

		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOnce)

		pvcInfo, err := pvc.New(clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Equal(t, clusterClient, pvcInfo.ClusterClient)
		assert.Equal(t, "test", pvcInfo.Claim.Name)
		assert.Equal(t, "testns", pvcInfo.Claim.Namespace)
		assert.Equal(t, "node-2", pvcInfo.MountedNode)
		assert.Equal(t, true, pvcInfo.SupportsRWO)
		assert.Equal(t, false, pvcInfo.SupportsROX)
		assert.Equal(t, false, pvcInfo.SupportsRWX)
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

		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOnce, corev1.ReadOnlyMany)

		pvcInfo, err := pvc.New(clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Equal(t, clusterClient, pvcInfo.ClusterClient)
		assert.Equal(t, "test", pvcInfo.Claim.Name)
		assert.Equal(t, "testns", pvcInfo.Claim.Namespace)
		assert.Equal(t, "node-2", pvcInfo.MountedNode)
		assert.Equal(t, true, pvcInfo.SupportsRWO)
		assert.Equal(t, true, pvcInfo.SupportsROX)
		assert.Equal(t, false, pvcInfo.SupportsRWX)
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

		clusterClient := buildClusterClient("node-2", corev1.ReadWriteOncePod)

		_, err := pvc.New(clusterClient, "testns", "test")
		require.ErrorContains(t, err, "ReadWriteOncePod")
	})

	t.Run("ReadWriteOncePod with no mounting pod is supported", func(t *testing.T) {
		t.Parallel()

		clusterClient := buildClusterClient("", corev1.ReadWriteOncePod)

		pvcInfo, err := pvc.New(clusterClient, "testns", "test")
		require.NoError(t, err)

		assert.Equal(t, "", pvcInfo.MountedNode)
	})
}

func buildClusterClient(mountingNode string, pvcAccessModes ...corev1.PersistentVolumeAccessMode) *k8s.ClusterClient {
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
	kubeClient := fake.NewSimpleClientset(objects...)

	return &k8s.ClusterClient{
		KubeClient: kubeClient,
	}
}
