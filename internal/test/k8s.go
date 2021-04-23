package test

import (
	// for go:embed directive to work
	_ "embed"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"os"
)

//go:embed _kubeconfig.yaml
var kubeconfig string

func PrepareKubeconfig() string {
	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)
	return testConfig.Name()
}

func DeleteKubeconfig(path string) {
	_ = os.Remove(path)
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

func NS(name string) corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
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

func Deployment(objectMeta metav1.ObjectMeta, pvc string) *appsv1.Deployment {
	terminationGracePeriodSeconds := int64(0)
	labels := map[string]string{
		"pv-migrate-app": objectMeta.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: objectMeta,
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					Volumes: []corev1.Volume{
						{Name: "volume", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvc,
							},
						}},
					},
					Containers: []corev1.Container{
						{
							Name:  "pod",
							Image: "docker.io/busybox:stable",
							Command: []string{
								"tail",
								"-f",
								"/dev/null",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "volume",
									MountPath: "/volume",
								},
							},
						},
					},
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

type KubernetesClientProvider struct {
	Objects []runtime.Object
}

func (m *KubernetesClientProvider) GetClientAndNsInContext(_ string, _ string) (kubernetes.Interface, string, error) {
	return fake.NewSimpleClientset(m.Objects...), "", nil
}
