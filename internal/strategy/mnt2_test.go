package strategy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
)

//nolint:dupl
func TestCanDoSameNode(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

//nolint:dupl
func TestCanDoDestRWX(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

//nolint:dupl
func TestCanDoSourceROX(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

//nolint:dupl
func TestCannotDoSameClusterDifferentNS(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.False(t, canDo)
}

func TestMnt2CannotDoDifferentCluster(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c1 := buildTestClient(pvcA, pvcB, podA, podB)
	c2 := buildTestClientWithAPIServerHost("https://127.0.0.2:6443", pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c1, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c2, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.False(t, canDo)
}

//nolint:dupl
func TestDetermineTargetNodeROXToTWO(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, destNode, targetNode)
}

//nolint:dupl
func TestDetermineTargetNodeRWOToRWX(t *testing.T) {
	t.Parallel()

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

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(context.Background(), c, sourceNS, sourcePVC)
	dst, _ := pvc.New(context.Background(), c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, sourceNode, targetNode)
}

func buildTestClient(objects ...runtime.Object) *k8s.ClusterClient {
	return buildTestClientWithAPIServerHost("https://127.0.0.1:6443", objects...)
}

func buildTestClientWithAPIServerHost(apiServerHost string,
	objects ...runtime.Object,
) *k8s.ClusterClient {
	return &k8s.ClusterClient{
		KubeClient:       fake.NewSimpleClientset(objects...),
		RESTClientGetter: nil,
		NsInContext:      "",
		RestConfig: &rest.Config{
			Host: apiServerHost,
		},
	}
}
