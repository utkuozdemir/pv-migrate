package migration

import (
	"errors"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestNewEngineEmptyStrategies(t *testing.T) {
	_, err := NewEngine([]Strategy{})
	if err == nil {
		t.Fatal("expected error for empty list of strategies")
	}
}

func TestNewEngineDuplicateStrategies(t *testing.T) {
	strategy1 := testStrategy{
		name: "strategy1",
	}
	strategy2 := testStrategy{
		name: "strategy1",
	}
	strategies := []Strategy{&strategy1, &strategy2}
	_, err := NewEngine(strategies)
	if err == nil {
		t.Fatal("expected error for duplicate strategies")
	}
}

func TestValidateRequestWithNonExistingStrategy(t *testing.T) {
	eng := testEngine(testStrategies()...)
	request := request{
		strategies: []string{"strategy3"},
	}

	err := eng.validate(&request)
	if err == nil {
		t.Fatal("expected error for non existing strategy")
	}
}

func TestBuildTask(t *testing.T) {
	testEngine := testEngine(testStrategies()...)
	testRequest := testRequest()
	task, err := testEngine.buildTask(testRequest)
	assert.Nil(t, err)

	assert.Len(t, task.Id(), 5)

	assert.True(t, task.Options().DeleteExtraneousFiles())
	assert.Equal(t, "namespace1", task.Source().Claim().Namespace)
	assert.Equal(t, "pvc1", task.Source().Claim().Name)
	assert.Equal(t, "node1", task.Source().MountedNode())
	assert.False(t, task.Source().SupportsRWO())
	assert.True(t, task.Source().SupportsROX())
	assert.False(t, task.Source().SupportsRWX())
	assert.Equal(t, "namespace2", task.Dest().Claim().Namespace)
	assert.Equal(t, "pvc2", task.Dest().Claim().Name)
	assert.Equal(t, "node2", task.Dest().MountedNode())
	assert.True(t, task.Dest().SupportsRWO())
	assert.False(t, task.Dest().SupportsROX())
	assert.True(t, task.Dest().SupportsRWX())
}

func TestFindStrategies(t *testing.T) {
	mockEngine := testEngine(testStrategies()...)
	allStrategies, _ := mockEngine.findStrategies("strategy1", "strategy2")
	assert.Len(t, allStrategies, 2)
	singleStrategy, _ := mockEngine.findStrategies("strategy1")
	assert.Len(t, singleStrategy, 1)
	assert.Equal(t, singleStrategy[0].Name(), "strategy1")
	emptyStrategies, _ := mockEngine.findStrategies()
	assert.Empty(t, emptyStrategies)
}

func TestDetermineStrategies(t *testing.T) {
	engine := testEngine(testStrategies()...)
	request := testRequest()
	task, _ := engine.buildTask(request)
	strategies, _ := engine.determineStrategies(request, task)
	assert.Len(t, strategies, 2)
}

func TestDetermineStrategiesCorrectOrder(t *testing.T) {
	strategy1 := testStrategy{
		name:     "strategy1",
		canDo:    canDoTrue,
		priority: 3000,
	}
	strategy2 := testStrategy{
		name:     "strategy2",
		canDo:    canDoTrue,
		priority: 1000,
	}
	strategy3 := testStrategy{
		name:     "strategy3",
		canDo:    canDoTrue,
		priority: 2000,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	request := testRequest()
	task, _ := engine.buildTask(request)
	strategies, _ := engine.determineStrategies(request, task)
	assert.Len(t, strategies, 3)
	assert.Equal(t, "strategy2", strategies[0].Name())
	assert.Equal(t, "strategy3", strategies[1].Name())
	assert.Equal(t, "strategy1", strategies[2].Name())
}

func TestDetermineStrategiesCannotDo(t *testing.T) {
	strategy1 := testStrategy{
		name:     "strategy1",
		canDo:    canDoFalse,
		priority: 3000,
	}
	strategy2 := testStrategy{
		name:     "strategy2",
		canDo:    canDoTrue,
		priority: 1000,
	}

	engine := testEngine(&strategy1, &strategy2)
	request := testRequest()
	task, _ := engine.buildTask(request)
	strategies, _ := engine.determineStrategies(request, task)
	assert.Len(t, strategies, 1)
	assert.Equal(t, "strategy2", strategies[0].Name())
}

func TestDetermineStrategiesRequested(t *testing.T) {
	strategy1 := testStrategy{
		name:     "strategy1",
		canDo:    canDoTrue,
		priority: 3000,
	}
	strategy2 := testStrategy{
		name:     "strategy2",
		canDo:    canDoTrue,
		priority: 1000,
	}
	strategy3 := testStrategy{
		name:     "strategy3",
		canDo:    canDoTrue,
		priority: 2000,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	request := testRequest("strategy1", "strategy3")
	task, _ := engine.buildTask(request)
	strategies, _ := engine.determineStrategies(request, task)
	assert.Len(t, strategies, 2)
	assert.Equal(t, "strategy1", strategies[0].Name())
	assert.Equal(t, "strategy3", strategies[1].Name())
}

func TestDetermineStrategiesRequestedNonExistent(t *testing.T) {
	strategy1 := testStrategy{
		name:     "strategy1",
		canDo:    canDoTrue,
		priority: 3000,
	}

	engine := testEngine(&strategy1)
	request := testRequest("strategy1", "strategy2")
	task, _ := engine.buildTask(request)
	strategies, err := engine.determineStrategies(request, task)
	assert.Nil(t, strategies)
	assert.NotNil(t, err)
}

func TestRun(t *testing.T) {
	var called []string
	cleanup := func(t Task) error {
		return nil
	}
	strategy1 := testStrategy{
		name:     "strategy1",
		canDo:    canDoTrue,
		priority: 3000,
		run: func(t Task) error {
			called = append(called, "strategy1")
			return nil
		},
		cleanup: cleanup,
	}
	strategy2 := testStrategy{
		name:     "strategy2",
		canDo:    canDoTrue,
		priority: 1000,
		run: func(t Task) error {
			called = append(called, "strategy2")
			return errors.New("test error")
		},
		cleanup: cleanup,
	}
	strategy3 := testStrategy{
		name:     "strategy3",
		canDo:    canDoFalse,
		priority: 2000,
		run: func(t Task) error {
			called = append(called, "strategy3")
			return nil
		},
		cleanup: cleanup,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	request := testRequest()
	err := engine.Run(request)
	assert.Nil(t, err)
	assert.Len(t, called, 2)
	assert.Equal(t,"strategy2", called[0])
	assert.Equal(t,"strategy1", called[1])
}

func testRequest(strategies ...string) Request {
	source := NewRequestPvc("/kubeconfig1", "context1", "namespace1", "pvc1")
	dest := NewRequestPvc("/kubeconfig2", "context2", "namespace2", "pvc2")
	options := NewRequestOptions(true)
	newRequest := NewRequest(source, dest, options, strategies)
	return newRequest
}

func testEngine(strategies ...Strategy) Engine {
	pvcA := pvcWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := pvcWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce, v1.ReadWriteMany)
	podA := pod("namespace1", "pod1", "node1", "pvc1")
	podB := pod("namespace2", "pod2", "node2", "pvc2")
	kubernetesClientProvider := testKubernetesClientProvider{objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := NewEngineWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	return e
}

func testStrategies() []Strategy {
	strategy1 := testStrategy{
		name:  "strategy1",
		canDo: canDoTrue,
	}
	strategy2 := testStrategy{
		name:  "strategy2",
		canDo: canDoTrue,
	}
	strategies := []Strategy{&strategy1, &strategy2}
	return strategies
}

func canDoTrue(task Task) bool {
	return true
}

func canDoFalse(task Task) bool {
	return false
}

func pod(namespace string, name string, node string, pvc string) *v1.Pod {
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

func objectMeta(namespace string, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
	}
}

func pvcWithAccessModes(namespace string, name string, accessModes ...v1.PersistentVolumeAccessMode) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: objectMeta(namespace, name),
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
		},
	}
}
