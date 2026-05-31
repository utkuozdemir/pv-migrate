package migrator

import (
	"context"
	"log/slog"
	"testing"

	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
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

	ctx := t.Context()

	logger := slogt.New(t)

	m := Migrator{getKubeClient: fakeClusterClientGetter()}
	mig := buildMigration(true)
	tsk, err := m.buildMigration(ctx, mig, logger)
	require.NoError(t, err)

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

	ctx := t.Context()
	logger := slogt.New(t)

	m := Migrator{getKubeClient: fakeClusterClientGetter()}
	mig := buildMigration(false)
	tsk, err := m.buildMigration(ctx, mig, logger)
	assert.Nil(t, tsk)
	require.Error(t, err)
}

func TestBuildMigrationSizeCheck(t *testing.T) {
	t.Parallel()

	logger := slogt.New(t)

	t.Run("fails when destination is smaller than source", func(t *testing.T) {
		t.Parallel()

		m := Migrator{getKubeClient: fakeClusterClientGetterWithSizes("2Gi", "1Gi")}
		req := buildMigration(true)

		tsk, err := m.buildMigration(t.Context(), req, logger)
		assert.Nil(t, tsk)
		require.ErrorContains(t, err, "smaller than source")
		require.ErrorContains(t, err, "--ignore-sizes")
	})

	t.Run("succeeds when destination is smaller but --ignore-sizes is set", func(t *testing.T) {
		t.Parallel()

		m := Migrator{getKubeClient: fakeClusterClientGetterWithSizes("2Gi", "1Gi")}
		req := buildMigration(true)
		req.IgnoreSizes = true

		tsk, err := m.buildMigration(t.Context(), req, logger)
		require.NoError(t, err)
		assert.NotNil(t, tsk)
	})

	t.Run("succeeds when destination is larger than source", func(t *testing.T) {
		t.Parallel()

		m := Migrator{getKubeClient: fakeClusterClientGetterWithSizes("1Gi", "2Gi")}
		req := buildMigration(true)

		tsk, err := m.buildMigration(t.Context(), req, logger)
		require.NoError(t, err)
		assert.NotNil(t, tsk)
	})

	t.Run("succeeds when destination is smaller but its provisioner does not enforce capacity", func(t *testing.T) {
		t.Parallel()

		m := Migrator{getKubeClient: fakeClusterClientGetterWithProvisioner("2Gi", "1Gi", "rancher.io/local-path")}
		req := buildMigration(true)

		tsk, err := m.buildMigration(t.Context(), req, logger)
		require.NoError(t, err)
		assert.NotNil(t, tsk)
	})
}

func TestCapacityEnforced(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		provisioner string
		want        bool
	}{
		"rancher local-path":      {provisioner: "rancher.io/local-path", want: false},
		"minikube hostpath":       {provisioner: "k8s.io/minikube-hostpath", want: false},
		"microk8s hostpath":       {provisioner: "microk8s.io/hostpath", want: false},
		"docker desktop hostpath": {provisioner: "docker.io/hostpath", want: false},
		"kubevirt hostpath":       {provisioner: "kubevirt.io/hostpath-provisioner", want: false},
		"kubevirt hostpath csi":   {provisioner: "kubevirt.io.hostpath-provisioner", want: false},
		"openebs local":           {provisioner: "openebs.io/local", want: false},
		"aws ebs csi":             {provisioner: "ebs.csi.aws.com", want: true},
		"gce pd csi":              {provisioner: "pd.csi.storage.gke.io", want: true},
		"local static":            {provisioner: "local-volume-provisioner-node-abc123", want: true},
		"unknown empty":           {provisioner: "", want: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, capacityEnforced(tt.provisioner))
		})
	}
}

func TestRunStrategiesInOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	logger := slogt.New(t)

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
		getStrategyMap: func([]string) (map[string]strategy.Strategy, error) {
			return map[string]strategy.Strategy{
				"str1": &str1,
				"str2": &str2,
				"str3": &str3,
			}, nil
		},
	}

	strs := []string{"str3", "str1", "str2"}
	mig := buildMigrationRequestWithStrategies(strs, true)

	err := migrator.Run(ctx, mig, logger)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 1, 2}, result)
}

func buildMigration(ignoreMounted bool) *migration.Request {
	return buildMigrationRequestWithStrategies([]string{"mount", "clusterip", "loadbalancer"}, ignoreMounted)
}

func buildMigrationRequestWithStrategies(
	strategies []string,
	ignoreMounted bool,
) *migration.Request {
	return &migration.Request{
		Source: migration.PVCInfo{
			Namespace: sourceNS,
			Name:      sourcePVC,
		},
		Dest: migration.PVCInfo{
			Namespace: destNS,
			Name:      destPVC,
		},
		IgnoreMounted: ignoreMounted,
		Strategies:    strategies,
	}
}

func fakeClusterClientGetter() clusterClientGetter {
	return fakeClusterClientGetterWithSizes("512Mi", "512Mi")
}

func fakeClusterClientGetterWithSizes(sourceSize, destSize string) clusterClientGetter {
	return fakeClusterClientGetterWithProvisioner(sourceSize, destSize, "")
}

func fakeClusterClientGetterWithProvisioner(sourceSize, destSize, destProvisioner string) clusterClientGetter {
	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceSize, corev1.ReadOnlyMany)
	pvcB := buildTestPVC(destNS, destPVC, destSize, corev1.ReadWriteOnce, corev1.ReadWriteMany)

	if destProvisioner != "" {
		pvcB.Annotations = map[string]string{
			"volume.kubernetes.io/storage-provisioner": destProvisioner,
		}
	}

	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPVC)

	return func(string, string, *slog.Logger) (*k8s.ClusterClient, error) {
		return &k8s.ClusterClient{
			KubeClient: fake.NewClientset(pvcA, pvcB, podA, podB),
		}, nil
	}
}

func buildTestPod(ns, name, node, pvc string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
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

func buildTestPVC(
	ns, name, size string,
	accessModes ...corev1.PersistentVolumeAccessMode,
) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse(size),
				},
			},
		},
	}
}

type mockStrategy struct {
	runFunc func(context.Context, *migration.Attempt) error
}

func (m *mockStrategy) Run(ctx context.Context, attempt *migration.Attempt, _ *slog.Logger) error {
	return m.runFunc(ctx, attempt)
}
