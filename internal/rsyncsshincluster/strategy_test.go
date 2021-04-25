package rsyncsshincluster

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

func TestCanDoSameCluster(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := RsyncSSSHInCluster{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PVCWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace2", "pod2", "node2", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPVC(kubeconfig, "context1", "namespace2", "pvc2")
	request := request2.NewWithDefaultImages(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildJob(request)
	canDo := strategy.CanDo(task)
	assert.True(t, canDo)
}

func TestCannotDoDifferentContext(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig)

	strategy := RsyncSSSHInCluster{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
	dest := request2.NewPVC(kubeconfig, "context2", "namespace1", "pvc2")
	request := request2.NewWithDefaultImages(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildJob(request)
	canDo := strategy.CanDo(task)
	assert.False(t, canDo)
}

func TestCannotDoDifferentKubeconfigs(t *testing.T) {
	kubeconfig1 := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig1)

	kubeconfig2 := test.PrepareKubeconfig()
	defer test.DeleteKubeconfig(kubeconfig2)

	strategy := RsyncSSSHInCluster{}
	strategies := []strategy2.Strategy{&strategy}
	pvcA := test.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
	pvcB := test.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := test.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := test.Pod("namespace1", "pod2", "node1", "pvc2")
	kubernetesClientProvider := test.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e, _ := engine.NewWithKubernetesClientProvider(strategies, &kubernetesClientProvider)
	source := request2.NewPVC(kubeconfig1, "context1", "namespace1", "pvc1")
	dest := request2.NewPVC(kubeconfig2, "context1", "namespace1", "pvc2")
	request := request2.NewWithDefaultImages(source, dest, request2.NewOptions(true), []string{})
	task, _ := e.BuildJob(request)
	canDo := strategy.CanDo(task)
	assert.False(t, canDo)
}

func TestNameConstant(t *testing.T) {
	strategy := RsyncSSSHInCluster{}
	name1 := strategy.Name()
	name2 := strategy.Name()
	assert.Equal(t, name1, name2)
}

func TestPriorityConstant(t *testing.T) {
	strategy := RsyncSSSHInCluster{}
	priority1 := strategy.Priority()
	priority2 := strategy.Priority()
	assert.Equal(t, priority1, priority2)
}
