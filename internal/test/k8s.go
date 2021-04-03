package test

import (
	_ "embed"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
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

func objectMeta(namespace string, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
	}
}

func PvcWithAccessModes(namespace string, name string, accessModes ...v1.PersistentVolumeAccessMode) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: objectMeta(namespace, name),
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
		},
	}
}

func Pod(namespace string, name string, node string, pvc string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: objectMeta(namespace, name),
		Spec: v1.PodSpec{
			NodeName: node,
			Volumes: []v1.Volume{
				{Name: "a", VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
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

func (m *KubernetesClientProvider) GetKubernetesClient(_ string, _ string) (kubernetes.Interface, error) {
	return fake.NewSimpleClientset(m.Objects...), nil
}
