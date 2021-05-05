package strategy

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/testutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestCanDoSameNode(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node1"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&tsk)
	assert.True(t, canDo)
}

func TestCanDoDestRWX(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteMany}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&tsk)
	assert.True(t, canDo)
}

func TestCanDoSourceROX(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&tsk)
	assert.True(t, canDo)
}

func TestCannotDoSameClusterDifferentNS(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

	destNS := "namespace2"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node1"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&tsk)
	assert.False(t, canDo)
}

func TestMnt2CannotDoDifferentCluster(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c1 := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	c2 := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c1, sourceNS, sourcePVC)
	dst, _ := pvc.New(c2, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&tsk)
	assert.False(t, canDo)
}

func TestDetermineTargetNodeROXToTWO(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&tsk)
	assert.Equal(t, destNode, targetNode)
}

func TestDetermineTargetNodeRWOToRWX(t *testing.T) {
	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteMany}

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	tsk := task.Task{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&tsk)
	assert.Equal(t, sourceNode, targetNode)
}
