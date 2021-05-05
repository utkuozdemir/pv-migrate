package testutil

import (
	// for go:embed directive to work
	_ "embed"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed _kubeconfig.yaml
var kubeconfig string

func PrepareKubeconfig() string {
	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)
	return testConfig.Name()
}

func ObjectMeta(namespace string, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
	}
}

func PVCWithAccessModes(namespace string, name string,
	accessModes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
	return PVC(ObjectMeta(namespace, name), "512Mi", accessModes...)
}

func PVC(objectMeta metav1.ObjectMeta, capacity string,
	accessModes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: objectMeta,
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse(capacity),
				},
			},
		},
	}
}

func Pod(namespace string, name string, node string, pvc string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: ObjectMeta(namespace, name),
		Spec: corev1.PodSpec{
			NodeName: node,
			Volumes: []corev1.Volume{
				{Name: "a", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvc,
					},
				}},
			},
		},
	}
}
