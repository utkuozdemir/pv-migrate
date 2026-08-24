package strategy

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func buildTestPod(ns, name, node, pvc string) *corev1.Pod {
	return &corev1.Pod{
		Namespace: ns,
		Name:      name,
		Spec: corev1.PodSpec{
			NodeName: node,
			Volumes: []corev1.Volume{
				{
					Name: "a",
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc,
					},
				},
			},
		},
	}
}

func buildTestPVC(ns, name string, accessModes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		Namespace: ns,
		Name:      name,
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse("512Mi"),
				},
			},
		},
	}
}
