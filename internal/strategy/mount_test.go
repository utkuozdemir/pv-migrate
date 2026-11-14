//nolint:dupl
package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
	assert.False(t, canDo)
}

func TestMountCannotDoDifferentCluster(t *testing.T) {
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

	s := Mount{}
	canDo := s.cannotDoReason(&mig) == ""
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

func TestBuildMountHelmValuesUsesRequiredTargetNodeAffinity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("pins to target node with required nodeAffinity instead of nodeName", func(t *testing.T) {
		t.Parallel()

		pvcA := buildTestPVC("namespace1", "pvc1", v1.ReadOnlyMany)
		pvcB := buildTestPVC("namespace1", "pvc2", v1.ReadWriteOnce)
		podA := buildTestPod("namespace1", "pod1", "node1", "pvc1")
		podB := buildTestPod("namespace1", "pod2", "node2", "pvc2")
		c := buildTestClient(pvcA, pvcB, podA, podB)

		src, err := pvc.New(ctx, c, "namespace1", "pvc1")
		require.NoError(t, err)

		dst, err := pvc.New(ctx, c, "namespace1", "pvc2")
		require.NoError(t, err)

		mig := migration.Migration{
			Request:    &migration.Request{},
			SourceInfo: src,
			DestInfo:   dst,
		}

		vals := buildMountHelmValues(&mig, "rsync -a /source/ /dest/")
		rsyncVals, ok := vals[rsyncComponent].(map[string]any)
		require.True(t, ok)

		assert.NotContains(t, rsyncVals, "nodeName")
		assert.Equal(t, pvc.RequiredNodeAffinity("node2"), rsyncVals[keyAffinity])
	})

	t.Run("sets nil affinity and omits nodeName when both claims are unmounted", func(t *testing.T) {
		t.Parallel()

		pvcA := buildTestPVC("namespace1", "pvc1", v1.ReadWriteOnce)
		pvcB := buildTestPVC("namespace1", "pvc2", v1.ReadWriteOnce)
		c := buildTestClient(pvcA, pvcB)

		src, err := pvc.New(ctx, c, "namespace1", "pvc1")
		require.NoError(t, err)

		dst, err := pvc.New(ctx, c, "namespace1", "pvc2")
		require.NoError(t, err)

		mig := migration.Migration{
			Request:    &migration.Request{},
			SourceInfo: src,
			DestInfo:   dst,
		}

		vals := buildMountHelmValues(&mig, "rsync -a /source/ /dest/")
		rsyncVals, ok := vals[rsyncComponent].(map[string]any)
		require.True(t, ok)

		assert.NotContains(t, rsyncVals, "nodeName")
		assert.Nil(t, rsyncVals[keyAffinity])
	})
}

func TestCannotDoCrossNodeROXSourceMountReadWrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	pvcA := buildTestPVC("namespace1", "pvc1", v1.ReadWriteOnce, v1.ReadOnlyMany)
	pvcB := buildTestPVC("namespace1", "pvc2", v1.ReadWriteOnce)
	podA := buildTestPod("namespace1", "pod1", "node1", "pvc1")
	podB := buildTestPod("namespace1", "pod2", "node2", "pvc2")
	c := buildTestClient(pvcA, pvcB, podA, podB)

	src, err := pvc.New(ctx, c, "namespace1", "pvc1")
	require.NoError(t, err)

	dst, err := pvc.New(ctx, c, "namespace1", "pvc2")
	require.NoError(t, err)

	mig := migration.Migration{
		Request:    &migration.Request{SourceMountReadWrite: true},
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Mount{}
	assert.NotEmpty(t, s.cannotDoReason(&mig))
}

func TestDetermineTargetNodeDestRWOAndROX(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	pvcA := buildTestPVC("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := buildTestPVC("namespace1", "pvc2", v1.ReadWriteOnce, v1.ReadOnlyMany)
	podA := buildTestPod("namespace1", "pod1", "node1", "pvc1")
	podB := buildTestPod("namespace1", "pod2", "node2", "pvc2")
	c := buildTestClient(pvcA, pvcB, podA, podB)

	src, err := pvc.New(ctx, c, "namespace1", "pvc1")
	require.NoError(t, err)

	dst, err := pvc.New(ctx, c, "namespace1", "pvc2")
	require.NoError(t, err)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	assert.Equal(t, "node2", determineTargetNode(&mig))
}

func TestTopologyUsesRequiredAffinityForReadWriteROXClaim(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	pvcA := buildTestPVC("namespace1", "pvc1", v1.ReadWriteOnce, v1.ReadOnlyMany)
	pvcB := buildTestPVC("namespace1", "pvc2", v1.ReadWriteOnce, v1.ReadOnlyMany)
	podA := buildTestPod("namespace1", "pod1", "node1", "pvc1")
	podB := buildTestPod("namespace1", "pod2", "node2", "pvc2")
	c := buildTestClient(pvcA, pvcB, podA, podB)

	src, err := pvc.New(ctx, c, "namespace1", "pvc1")
	require.NoError(t, err)

	dst, err := pvc.New(ctx, c, "namespace1", "pvc2")
	require.NoError(t, err)

	defaultMig := migration.Migration{
		Request:    &migration.Request{},
		SourceInfo: src,
		DestInfo:   dst,
	}
	defaultTopo := resolveTopology(&defaultMig)

	sshdDefault := buildSshdHelmValues(defaultTopo.sshd, "ssh-ed25519 AAAA")
	rsyncDefault := buildRsyncHelmValues(defaultTopo.rsync, "rsync", "key", "/tmp/id")

	assert.Equal(t, src.AffinityHelmValues, sshdDefault[keyAffinity])
	assert.Equal(t, pvc.RequiredNodeAffinity("node2"), rsyncDefault[keyAffinity])

	rwMig := migration.Migration{
		Request:    &migration.Request{SourceMountReadWrite: true},
		SourceInfo: src,
		DestInfo:   dst,
	}
	rwTopo := resolveTopology(&rwMig)

	sshdRW := buildSshdHelmValues(rwTopo.sshd, "ssh-ed25519 AAAA")
	assert.Equal(t, pvc.RequiredNodeAffinity("node1"), sshdRW[keyAffinity])
}

func buildTestClient(objects ...runtime.Object) *k8s.ClusterClient {
	return buildTestClientWithAPIServerHost("https://127.0.0.1:6443", objects...)
}

func buildTestClientWithAPIServerHost(apiServerHost string,
	objects ...runtime.Object,
) *k8s.ClusterClient {
	return &k8s.ClusterClient{
		KubeClient:       fake.NewClientset(objects...),
		RESTClientGetter: nil,
		NsInContext:      "",
		RestConfig: &rest.Config{
			Host: apiServerHost,
		},
	}
}
