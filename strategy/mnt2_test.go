//nolint:dupl
package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/pvc"
)

func TestCanDoSameNode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCanDoSourceUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := ""
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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCanDoDestUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := ""
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCanDoBothUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := ""
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := ""
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCanDoDestRWX(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCanDoSourceROX(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}

func TestCannotDoSameClusterDifferentNS(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

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

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c1, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c2, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mnt2{}
	canDo := s.canDo(&mig)
	assert.False(t, canDo)
}

func TestDetermineTargetNodeROXToRWO(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, destNode, targetNode)
}

func TestDetermineTargetNodeROXToRWOSourceUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := ""
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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, destNode, targetNode)
}

func TestDetermineTargetNodeROXToRWODestUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := ""
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, sourceNode, targetNode)
}

func TestDetermineTargetNodeRWOToRWX(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, sourceNode, targetNode)
}

func TestDetermineTargetNodeRWOToRWXSourceUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := ""
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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, destNode, targetNode)
}

func TestDetermineTargetNodeRWOToRWXDestUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := ""
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteMany}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, sourceNode, targetNode)
}

func TestDetermineTargetNodeSourceUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := ""
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

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
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	targetNode := determineTargetNode(&mig)
	assert.Equal(t, destNode, targetNode)
}

func TestDetermineTargetNodeDestUnmounted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace1"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := ""
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(ctx, c, sourceNS, sourcePVC)
	dst, _ := pvc.New(ctx, c, destNS, destPvc)

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
