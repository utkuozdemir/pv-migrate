package mountboth

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/engine"
	request2 "github.com/utkuozdemir/pv-migrate/internal/request"
	strategy2 "github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestCanDoSameClusterSameNsSameNode(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.True(t, canDo)
}

func TestCanDoSameClusterSameNsDestRwx(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.True(t, canDo)
}

func TestCanDoSameClusterSameNsSourceRox(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.True(t, canDo)
}

func TestCannotDoSameClusterDifferentNs(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace2", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace2", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.False(t, canDo)
}

func TestCannotDoDifferentContext(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context2", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.False(t, canDo)
}

func TestCannotDoDifferentKubeconfigs(t *testing.T) {
	kubeconfig1 := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig1)

	kubeconfig2 := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig2)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig1, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig2, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	canDo := strategy.CanDo(task)
	assert.False(t, canDo)
}

func TestDetermineTargetNodeDestRWO(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	targetNode := determineTargetNode(task)
	assert.Equal(t, "node2", targetNode)
}

func TestDetermineTargetNodeSourceRWO(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteMany)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	targetNode := determineTargetNode(task)
	assert.Equal(t, "", targetNode)
}

func TestDetermineTargetNodeBothRWX(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	targetNode := determineTargetNode(task)
	assert.Equal(t, "node1", targetNode)
}

func TestNameConstant(t *testing.T) {
	strategy := MountBoth{}
	name1 := strategy.Name()
	name2 := strategy.Name()
	assert.Equal(t, name1, name2)
}

func TestPriorityConstant(t *testing.T) {
	strategy := MountBoth{}
	priority1 := strategy.Priority()
	priority2 := strategy.Priority()
	assert.Equal(t, priority1, priority2)
}

func TestBuildRsyncJob(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := MountBoth{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PvcWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PvcWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPvc(kubeconfig, "context1", "namespace1", "pvc2")
	request := request2.New(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildTask(request)
	targetNode := determineTargetNode(task)
	rsyncJob := buildRsyncJob(task, targetNode)
	jobTemplate := rsyncJob.Spec.Template
	podSpec := jobTemplate.Spec
	container := podSpec.Containers[0]
	assert.Len(t, container.VolumeMounts, 2)
	assert.Len(t, podSpec.Volumes, 2)

	pvcNames := extractPvcNamesFromVolumes(podSpec.Volumes)
	assert.EqualValues(t, []string{"pvc1", "pvc2"}, pvcNames)
}

func extractPvcNamesFromVolumes(volumes []v1.Volume) []string {
	var names []string
	for _, volume := range volumes {
		names = append(names, volume.PersistentVolumeClaim.ClaimName)
	}
	return names
}
