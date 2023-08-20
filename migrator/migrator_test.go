package migrator

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/strategy"
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
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := Migrator{getKubeClient: fakeClusterClientGetter()}
	mig := buildMigration(true)
	tsk, err := m.buildMigration(ctx, mig)
	assert.Nil(t, err)

	sourceInfo := tsk.SourceInfo
	destInfo := tsk.DestInfo

	assert.Equal(t, "namespace1", sourceInfo.Claim.Namespace)
	assert.Equal(t, "pvc1", sourceInfo.Claim.Name)
	assert.Equal(t, "node1", sourceInfo.MountedNode)
	assert.False(t, sourceInfo.SupportsRWO)
	assert.True(t, sourceInfo.SupportsROX)
	assert.False(t, sourceInfo.SupportsRWX)
	assert.Equal(t, "namespace2", destInfo.Claim.Namespace)
	assert.Equal(t, "pvc2", destInfo.Claim.Name)
	assert.Equal(t, "node2", destInfo.MountedNode)
	assert.True(t, destInfo.SupportsRWO)
	assert.False(t, destInfo.SupportsROX)
	assert.True(t, destInfo.SupportsRWX)
}

func TestBuildTaskMounted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := Migrator{getKubeClient: fakeClusterClientGetter()}
	mig := buildMigration(false)
	tsk, err := m.buildMigration(ctx, mig)
	assert.Nil(t, tsk)
	assert.Error(t, err)
}

func TestRunStrategiesInOrder(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var result []int

	str1 := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			result = append(result, 1)

			return strategy.ErrUnaccepted
		},
	}

	str2 := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			result = append(result, 2)

			return nil
		},
	}

	str3 := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			result = append(result, 3)

			return strategy.ErrUnaccepted
		},
	}

	migrator := Migrator{
		getKubeClient: fakeClusterClientGetter(),
		getStrategyMap: func(names []string) (map[string]strategy.Strategy, error) {
			return map[string]strategy.Strategy{
				"str1": &str1,
				"str2": &str2,
				"str3": &str3,
			}, nil
		},
	}

	strs := []string{"str3", "str1", "str2"}
	mig := buildMigrationRequestWithStrategies(strs, true)

	err := migrator.Run(ctx, mig)
	assert.NoError(t, err)
	assert.Equal(t, []int{3, 1, 2}, result)
}

func buildMigration(ignoreMounted bool) *migration.Request {
	return buildMigrationRequestWithStrategies(strategy.DefaultStrategies, ignoreMounted)
}

func buildMigrationRequestWithStrategies(strategies []string, ignoreMounted bool) *migration.Request {
	return &migration.Request{
		Source: &migration.PVCInfo{
			Namespace: sourceNS,
			Name:      sourcePVC,
		},
		Dest: &migration.PVCInfo{
			Namespace: destNS,
			Name:      destPVC,
		},
		IgnoreMounted: ignoreMounted,
		Strategies:    strategies,
		Logger:        log.NewEntry(log.New()),
	}
}

func fakeClusterClientGetter() clusterClientGetter {
	pvcA := buildTestPVC(sourceNS, sourcePVC, corev1.ReadOnlyMany)
	pvcB := buildTestPVC(destNS, destPVC, corev1.ReadWriteOnce, corev1.ReadWriteMany)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPVC)

	return func(kubeconfigPath string, context string) (*k8s.ClusterClient, error) {
		return &k8s.ClusterClient{
			KubeClient: fake.NewSimpleClientset(pvcA, pvcB, podA, podB),
		}, nil
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
	accessModes ...corev1.PersistentVolumeAccessMode,
) *corev1.PersistentVolumeClaim {
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

type mockStrategy struct {
	runFunc func(context.Context, *migration.Attempt) error
}

func (m *mockStrategy) Run(ctx context.Context, a *migration.Attempt) error {
	return m.runFunc(ctx, a)
}
