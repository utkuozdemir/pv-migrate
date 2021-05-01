package strategy

//import (
//	"github.com/stretchr/testify/assert"
//	"github.com/utkuozdemir/pv-migrate/internal/engine"
//	"github.com/utkuozdemir/pv-migrate/internal/request"
//	"github.com/utkuozdemir/pv-migrate/internal/testutil"
//	v1 "k8s.io/api/core/v1"
//	"k8s.io/apimachinery/pkg/runtime"
//	"testing"
//)
//
//func TestCanDoSameCluster(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Svc{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace2", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace2", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	task, _ := e.BuildJob(r)
//	canDo := s.CanDo(task)
//	assert.True(t, canDo)
//}
//
//func TestCannotDoDifferentContext(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Svc{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context2", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	task, _ := e.BuildJob(r)
//	canDo := s.CanDo(task)
//	assert.False(t, canDo)
//}
//
//func TestCannotDoDifferentKubeconfigs(t *testing.T) {
//	kubeconfig1 := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig1)
//
//	kubeconfig2 := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig2)
//
//	s := Svc{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig1, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig2, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	task, _ := e.BuildJob(r)
//	canDo := s.CanDo(task)
//	assert.False(t, canDo)
//}
//
//func TestNameConstant(t *testing.T) {
//	s := Svc{}
//	name1 := s.Name()
//	name2 := s.Name()
//	assert.Equal(t, name1, name2)
//}
