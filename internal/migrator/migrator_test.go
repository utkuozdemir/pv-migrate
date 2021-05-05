package migrator

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/migration"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

const (
	sourceNS   = "namespace1"
	destNS     = "namespace2"
	sourcePVC  = "pvc1"
	destPVC    = "pvc2"
	sourcePod  = "pod1"
	destPod    = "pod2"
	sourceNode = "node1"
	destNode   = "node2"
)

func TestBuildTask(t *testing.T) {
	m := migrator{getKubeClient: fakeKubeClientGetter()}
	mig := buildMigration(&migration.Options{IgnoreMounted: true})
	tsk, err := m.buildTask(mig)
	assert.Nil(t, err)

	s := tsk.SourceInfo
	d := tsk.DestInfo
	assert.Equal(t, "namespace1", s.Claim.Namespace)
	assert.Equal(t, "pvc1", s.Claim.Name)
	assert.Equal(t, "node1", s.MountedNode)
	assert.False(t, s.SupportsRWO)
	assert.True(t, s.SupportsROX)
	assert.False(t, s.SupportsRWX)
	assert.Equal(t, "namespace2", d.Claim.Namespace)
	assert.Equal(t, "pvc2", d.Claim.Name)
	assert.Equal(t, "node2", d.MountedNode)
	assert.True(t, d.SupportsRWO)
	assert.False(t, d.SupportsROX)
	assert.True(t, d.SupportsRWX)
}

func TestBuildTaskMounted(t *testing.T) {
	m := migrator{getKubeClient: fakeKubeClientGetter()}
	mig := buildMigration(&migration.Options{})
	tsk, err := m.buildTask(mig)
	assert.Nil(t, tsk)
	assert.Error(t, err)
}

func buildMigration(options *migration.Options) *migration.Migration {
	return &migration.Migration{
		Source: &migration.PVC{
			Namespace: sourceNS,
			Name:      sourcePVC,
		},
		Dest: &migration.PVC{
			Namespace: destNS,
			Name:      destPVC,
		},
		Options:    options,
		Strategies: strategy.DefaultStrategies,
		RsyncImage: migration.DefaultRsyncImage,
		SshdImage:  migration.DefaultSshdImage,
	}
}

func fakeKubeClientGetter() kubeClientGetter {
	pvcA := buildTestPVC(sourceNS, sourcePVC, v1.ReadOnlyMany)
	pvcB := buildTestPVC(destNS, destPVC, v1.ReadWriteOnce, v1.ReadWriteMany)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPVC)

	return func(kubeconfigPath string, context string) (kubernetes.Interface, string, error) {
		return fake.NewSimpleClientset(pvcA, pvcB, podA, podB), "", nil
	}
}

func buildTestPod(namespace string, name string, node string, pvc string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
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

func buildTestPVC(namespace string, name string,
	accessModes ...corev1.PersistentVolumeAccessMode) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse("512Mi"),
				},
			},
		},
	}
}
