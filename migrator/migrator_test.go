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

// TestNodePortStrategyIntegration tests the integration of the NodePort strategy with the migrator
func TestNodePortStrategyIntegration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slogt.New(t)
	
	// Setup strategy mocks and migrator
	strategyExecutionOrder, migrator := setupNodePortStrategyTest()

	// Request with default strategies (should include NodePort now)
	// Important: We need to set ignoreMounted to true since our mocks use mounted PVCs
	req := buildMigration(true) 
	
	// Run the migration
	err := migrator.Run(ctx, req, logger)
	
	// Verify results
	require.NoError(t, err)
	
	assertNodePortStrategyExecution(t, strategyExecutionOrder)
}

// setupNodePortStrategyTest creates mock strategies and returns the execution order tracker and migrator
func setupNodePortStrategyTest() ([]string, Migrator) {
	strategyExecutionOrder := []string{}
	
	// Create mocks for each strategy
	mnt2Mock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			strategyExecutionOrder = append(strategyExecutionOrder, strategy.Mnt2Strategy)
			return strategy.ErrUnaccepted
		},
	}
	
	svcMock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			strategyExecutionOrder = append(strategyExecutionOrder, strategy.SvcStrategy)
			return strategy.ErrUnaccepted
		},
	}
	
	lbsvcMock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			strategyExecutionOrder = append(strategyExecutionOrder, strategy.LbSvcStrategy)
			return strategy.ErrUnaccepted
		},
	}
	
	nodeportMock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			strategyExecutionOrder = append(strategyExecutionOrder, strategy.NodePortStrategy)
			return nil // This one succeeds
		},
	}
	
	localMock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			strategyExecutionOrder = append(strategyExecutionOrder, strategy.LocalStrategy)
			return strategy.ErrUnaccepted
		},
	}
	
	migrator := Migrator{
		getKubeClient: fakeClusterClientGetter(),
		getStrategyMap: func([]string) (map[string]strategy.Strategy, error) {
			return map[string]strategy.Strategy{
				strategy.Mnt2Strategy:     &mnt2Mock,
				strategy.SvcStrategy:      &svcMock,
				strategy.LbSvcStrategy:    &lbsvcMock,
				strategy.NodePortStrategy: &nodeportMock,
				strategy.LocalStrategy:    &localMock,
			}, nil
		},
	}
	
	return strategyExecutionOrder, migrator
}

// assertNodePortStrategyExecution verifies the strategy execution
func assertNodePortStrategyExecution(t *testing.T, strategyExecutionOrder []string) {
	// Check that NodePort strategy was tried and succeeded
	assert.Contains(t, strategyExecutionOrder, strategy.NodePortStrategy, 
		"NodePort strategy should have been executed")
	
	// Check the execution order
	for i, s := range strategy.DefaultStrategies {
		if i < len(strategyExecutionOrder) {
			assert.Equal(t, s, strategyExecutionOrder[i],
				"Strategy execution order should match DefaultStrategies")
		}
	}
	
	// NodePort should have succeeded, so subsequent strategies shouldn't be tried
	assert.NotContains(t, strategyExecutionOrder, strategy.LocalStrategy,
		"LocalStrategy should not have been tried since NodePort succeeded")
}

// TestExplicitNodePortStrategySelection tests that NodePort strategy can be explicitly selected
func TestExplicitNodePortStrategySelection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slogt.New(t)

	nodeportExecuted := false
	
	nodeportMock := mockStrategy{
		runFunc: func(_ context.Context, _ *migration.Attempt) error {
			nodeportExecuted = true
			return nil
		},
	}
	
	migrator := Migrator{
		getKubeClient: fakeClusterClientGetter(),
		getStrategyMap: func([]string) (map[string]strategy.Strategy, error) {
			return map[string]strategy.Strategy{
				strategy.NodePortStrategy: &nodeportMock,
			}, nil
		},
	}

	// Request with only NodePort strategy explicitly selected
	// Important: We need to set ignoreMounted to true since our mocks use mounted PVCs
	req := buildMigrationRequestWithStrategies([]string{strategy.NodePortStrategy}, true) // Changed from false to true
	
	// Run the migration
	err := migrator.Run(ctx, req, logger)
	
	// Verify results
	require.NoError(t, err)
	assert.True(t, nodeportExecuted, "NodePort strategy should have been executed when explicitly selected")
}

func buildMigration(ignoreMounted bool) *migration.Request {
	return buildMigrationRequestWithStrategies(strategy.DefaultStrategies, ignoreMounted)
}

func buildMigrationRequestWithStrategies(
	strategies []string,
	ignoreMounted bool,
) *migration.Request {
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
	}
}

func fakeClusterClientGetter() clusterClientGetter {
	pvcA := buildTestPVC(sourceNS, sourcePVC, corev1.ReadOnlyMany)
	pvcB := buildTestPVC(destNS, destPVC, corev1.ReadWriteOnce, corev1.ReadWriteMany)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPVC)

	return func(string, string, *slog.Logger) (*k8s.ClusterClient, error) {
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
			Resources: corev1.VolumeResourceRequirements{
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

func (m *mockStrategy) Run(ctx context.Context, attempt *migration.Attempt, _ *slog.Logger) error {
	return m.runFunc(ctx, attempt)
}
