package strategy

//
//import (
//	"github.com/stretchr/testify/assert"
//	"github.com/utkuozdemir/pv-migrate/internal/engine"
//	"github.com/utkuozdemir/pv-migrate/internal/request"
//	"github.com/utkuozdemir/pv-migrate/internal/task"
//	"github.com/utkuozdemir/pv-migrate/internal/testutil"
//	v1 "k8s.io/api/core/v1"
//	"k8s.io/apimachinery/pkg/runtime"
//	"testing"
//)
//
//func TestCanDoSameClusterSameNsSameNode(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.True(t, canDo)
//}
//
//func TestCanDoSameClusterSameNsDestRwx(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.True(t, canDo)
//}
//
//func TestCanDoSameClusterSameNsSourceRox(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.True(t, canDo)
//}
//
//func TestCannotDoSameClusterDifferentNs(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace2", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace2", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.False(t, canDo)
//}
//
//func TestMnt2CannotDoDifferentContext(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context2", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.False(t, canDo)
//}
//
//func TestMnt2CannotDoDifferentKubeconfigs(t *testing.T) {
//	kubeconfig1 := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig1)
//
//	kubeconfig2 := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig2)
//
//	s := Mnt2{}
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig1, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig2, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	canDo := s.CanDo(j)
//	assert.False(t, canDo)
//}
//
//func TestDetermineTargetNodeDestRWO(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	job, _ := e.BuildJob(r)
//	targetNode := determineTargetNode(job)
//	assert.Equal(t, "node2", targetNode)
//}
//
//func TestDetermineTargetNodeSourceRWO(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteMany)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	targetNode := determineTargetNode(j)
//	assert.Equal(t, "", targetNode)
//}
//
//func TestDetermineTargetNodeBothRWX(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteMany)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node2", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	targetNode := determineTargetNode(j)
//	assert.Equal(t, "node1", targetNode)
//}
//
//func TestBuildRsyncJob(t *testing.T) {
//	kubeconfig := testutil.PrepareKubeconfig()
//	defer testutil.DeleteKubeconfig(kubeconfig)
//
//	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadWriteOnce)
//	pvcB := testutil.PVCWithAccessModes("namespace1", "pvc2", v1.ReadWriteOnce)
//	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
//	podB := testutil.Pod("namespace1", "pod2", "node1", "pvc2")
//	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
//	e := engine.NewWithKubernetesClientProvider(&kubernetesClientProvider)
//	source := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc1")
//	dest := request.NewPVC(kubeconfig, "context1", "namespace1", "pvc2")
//	r := request.NewWithDefaultImages(source, dest, defaultRequestOptions, []string{})
//	j, _ := e.BuildJob(r)
//	tsk := task.New(j)
//	targetNode := determineTargetNode(j)
//	rsyncJob, err := buildRsyncJob(tsk, targetNode)
//	assert.NoError(t, err)
//	jobTemplate := rsyncJob.Spec.Template
//	podSpec := jobTemplate.Spec
//	container := podSpec.Containers[0]
//	assert.Len(t, container.VolumeMounts, 2)
//	assert.Len(t, podSpec.Volumes, 2)
//
//	pvcNames := extractPvcNamesFromVolumes(podSpec.Volumes)
//	assert.EqualValues(t, []string{"pvc1", "pvc2"}, pvcNames)
//}
//
//func extractPvcNamesFromVolumes(volumes []v1.Volume) []string {
//	var names []string
//	for _, volume := range volumes {
//		names = append(names, volume.PersistentVolumeClaim.ClaimName)
//	}
//	return names
//}
