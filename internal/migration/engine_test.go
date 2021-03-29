package migration

import "testing"

func TestNewEngineEmptyStrategies(t *testing.T) {
	_, err := NewEngine([]Strategy{})
	if err == nil {
		t.Fatal("expected error for empty list of strategies")
	}
}

func TestNewEngineDuplicateStrategies(t *testing.T) {
	strategy1 := testStrategy{
		name: "aaa",
	}
	strategy2 := testStrategy{
		name: "aaa",
	}
	strategies := []Strategy{&strategy1, &strategy2}
	_, err := NewEngine(strategies)
	if err == nil {
		t.Fatal("expected error for duplicate strategies")
	}
}

func TestValidateRequestWithNonExistingStrategy(t *testing.T) {
	strategy1 := testStrategy{
		name: "aaa",
	}
	strategy2 := testStrategy{
		name: "bbb",
	}
	strategies := []Strategy{&strategy1, &strategy2}
	e, _ := NewEngine(strategies)
	eng := e.(engine)
	request := request{
		source:     nil,
		dest:       nil,
		options:    nil,
		strategies: []string{"ccc"},
	}

	err := eng.validate(&request)
	if err == nil {
		t.Fatal("expected error for non existing strategy")
	}
}

// todo
//func TestDetermineStrategies(t *testing.T)  {
//	strategy1 := testStrategy{
//		name: "aaa",
//		canDo: true,
//	}
//	strategy2 := testStrategy{
//		name: "bbb",
//		canDo: false,
//	}
//	strategies := []Strategy{&strategy1, &strategy2}
//	e, _ := NewEngine(strategies)
//	eng := e.(engine)
//
//	request{
//		source:     nil,
//		dest:       nil,
//		options:    nil,
//		strategies: nil,
//	}
//
//	eng.determineStrategies()
//}

type testStrategy struct {
	name     string
	priority int
	canDo    bool
	run      func(Task) error
	cleanup  func(Task) error
}

func (t *testStrategy) Name() string {
	return t.name
}

func (t *testStrategy) Priority() int {
	return t.priority
}

func (t *testStrategy) CanDo(task Task) bool {
	return t.canDo
}

func (t *testStrategy) Run(task Task) error {
	return t.run(task)
}

func (t *testStrategy) Cleanup(task Task) error {
	return t.cleanup(task)
}
