package migration

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

type testKubernetesClientProvider struct {
	objects []runtime.Object
}

func (m *testKubernetesClientProvider) GetKubernetesClient(_ string, _ string) (kubernetes.Interface, error) {
	return fake.NewSimpleClientset(m.objects...), nil
}

type testStrategy struct {
	name     string
	priority int
	canDo    func(Task) bool
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
	return t.canDo(task)
}

func (t *testStrategy) Run(task Task) error {
	return t.run(task)
}

func (t *testStrategy) Cleanup(task Task) error {
	return t.cleanup(task)
}
