package engine

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestNewEngineEmptyStrategies(t *testing.T) {
	_, err := New([]strategy.Strategy{})
	if err == nil {
		t.Fatal("expected error for empty list of strategies")
	}
}

func TestNewEngineDuplicateStrategies(t *testing.T) {
	strategy1 := test.Strategy{
		NameVal: "strategy1",
	}
	strategy2 := test.Strategy{
		NameVal: "strategy1",
	}
	strategies := []strategy.Strategy{&strategy1, &strategy2}
	_, err := New(strategies)
	if err == nil {
		t.Fatal("expected error for duplicate strategies")
	}
}

func TestValidateRequestWithNonExistingStrategy(t *testing.T) {
	eng := testEngine(testStrategies()...)
	req := request.New(nil, nil, request.NewOptions(true), []string{"strategy3"})
	err := eng.validate(req)
	if err == nil {
		t.Fatal("expected error for non existing strategy")
	}
}

func TestBuildTask(t *testing.T) {
	testEngine := testEngine(testStrategies()...)
	testRequest := testRequest()
	testTask, err := testEngine.BuildTask(testRequest)
	assert.Nil(t, err)

	assert.Len(t, testTask.ID(), 5)

	assert.True(t, testTask.Options().DeleteExtraneousFiles())
	assert.Equal(t, "namespace1", testTask.Source().Claim().Namespace)
	assert.Equal(t, "pvc1", testTask.Source().Claim().Name)
	assert.Equal(t, "node1", testTask.Source().MountedNode())
	assert.False(t, testTask.Source().SupportsRWO())
	assert.True(t, testTask.Source().SupportsROX())
	assert.False(t, testTask.Source().SupportsRWX())
	assert.Equal(t, "namespace2", testTask.Dest().Claim().Namespace)
	assert.Equal(t, "pvc2", testTask.Dest().Claim().Name)
	assert.Equal(t, "node2", testTask.Dest().MountedNode())
	assert.True(t, testTask.Dest().SupportsRWO())
	assert.False(t, testTask.Dest().SupportsROX())
	assert.True(t, testTask.Dest().SupportsRWX())
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
	r := testRequest()
	testTask, _ := engine.BuildTask(r)
	strategies, _ := engine.determineStrategies(r, testTask)
	assert.Len(t, strategies, 2)
}

func TestDetermineStrategiesCorrectOrder(t *testing.T) {
	strategy1 := test.Strategy{
		NameVal:     "strategy1",
		CanDoVal:    canDoTrue,
		PriorityVal: 3000,
	}
	strategy2 := test.Strategy{
		NameVal:     "strategy2",
		CanDoVal:    canDoTrue,
		PriorityVal: 1000,
	}
	strategy3 := test.Strategy{
		NameVal:     "strategy3",
		CanDoVal:    canDoTrue,
		PriorityVal: 2000,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	req := testRequest()
	testTask, _ := engine.BuildTask(req)
	strategies, _ := engine.determineStrategies(req, testTask)
	assert.Len(t, strategies, 3)
	assert.Equal(t, "strategy2", strategies[0].Name())
	assert.Equal(t, "strategy3", strategies[1].Name())
	assert.Equal(t, "strategy1", strategies[2].Name())
}

func TestDetermineStrategiesCannotDo(t *testing.T) {
	strategy1 := test.Strategy{
		NameVal:     "strategy1",
		CanDoVal:    canDoFalse,
		PriorityVal: 3000,
	}
	strategy2 := test.Strategy{
		NameVal:     "strategy2",
		CanDoVal:    canDoTrue,
		PriorityVal: 1000,
	}

	engine := testEngine(&strategy1, &strategy2)
	req := testRequest()
	testTask, _ := engine.BuildTask(req)
	strategies, _ := engine.determineStrategies(req, testTask)
	assert.Len(t, strategies, 1)
	assert.Equal(t, "strategy2", strategies[0].Name())
}

func TestDetermineStrategiesRequested(t *testing.T) {
	strategy1 := test.Strategy{
		NameVal:     "strategy1",
		CanDoVal:    canDoTrue,
		PriorityVal: 3000,
	}
	strategy2 := test.Strategy{
		NameVal:     "strategy2",
		CanDoVal:    canDoTrue,
		PriorityVal: 1000,
	}
	strategy3 := test.Strategy{
		NameVal:     "strategy3",
		CanDoVal:    canDoTrue,
		PriorityVal: 2000,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	req := testRequest("strategy1", "strategy3")
	testTask, _ := engine.BuildTask(req)
	strategies, _ := engine.determineStrategies(req, testTask)
	assert.Len(t, strategies, 2)
	assert.Equal(t, "strategy1", strategies[0].Name())
	assert.Equal(t, "strategy3", strategies[1].Name())
}

func TestDetermineStrategiesRequestedNonExistent(t *testing.T) {
	strategy1 := test.Strategy{
		NameVal:     "strategy1",
		CanDoVal:    canDoTrue,
		PriorityVal: 3000,
	}

	engine := testEngine(&strategy1)
	req := testRequest("strategy1", "strategy2")
	testTask, _ := engine.BuildTask(req)
	strategies, err := engine.determineStrategies(req, testTask)
	assert.Nil(t, strategies)
	assert.NotNil(t, err)
}

func TestRun(t *testing.T) {
	var called []string
	cleanup := func(t task.Task) error {
		return nil
	}
	strategy1 := test.Strategy{
		NameVal:     "strategy1",
		CanDoVal:    canDoTrue,
		PriorityVal: 3000,
		RunFunc: func(t task.Task) error {
			called = append(called, "strategy1")
			return nil
		},
		CleanupFunc: cleanup,
	}
	strategy2 := test.Strategy{
		NameVal:     "strategy2",
		CanDoVal:    canDoTrue,
		PriorityVal: 1000,
		RunFunc: func(t task.Task) error {
			called = append(called, "strategy2")
			return errors.New("test error")
		},
		CleanupFunc: cleanup,
	}
	strategy3 := test.Strategy{
		NameVal:     "strategy3",
		CanDoVal:    canDoFalse,
		PriorityVal: 2000,
		RunFunc: func(t task.Task) error {
			called = append(called, "strategy3")
			return nil
		},
		CleanupFunc: cleanup,
	}

	engine := testEngine(&strategy1, &strategy2, &strategy3)
	req := testRequest()
	err := engine.Run(req)
	assert.Nil(t, err)
	assert.Len(t, called, 2)
	assert.Equal(t, "strategy2", called[0])
	assert.Equal(t, "strategy1", called[1])
}

func testRequest(strategies ...string) request.Request {
	source := request.NewPVC("/kubeconfig1", "context1", "namespace1", "pvc1")
	dest := request.NewPVC("/kubeconfig2", "context2", "namespace2", "pvc2")
	options := request.NewOptions(true)
	newRequest := request.New(source, dest, options, strategies)
	return newRequest
}

func testEngine(strategies ...strategy.Strategy) Engine {
	pvcA := test.PVCWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := test.PVCWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce, v1.ReadWriteMany)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace2", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	return e
}

func testStrategies() []strategy.Strategy {
	strategy1 := test.Strategy{
		NameVal:  "strategy1",
		CanDoVal: canDoTrue,
	}
	strategy2 := test.Strategy{
		NameVal:  "strategy2",
		CanDoVal: canDoTrue,
	}
	strategies := []strategy.Strategy{&strategy1, &strategy2}
	return strategies
}

func canDoTrue(_ task.Task) bool {
	return true
}

func canDoFalse(_ task.Task) bool {
	return false
}
