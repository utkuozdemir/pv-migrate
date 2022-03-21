package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
	v1 "k8s.io/api/core/v1"
)

func TestSvcCanDoSameCluster(t *testing.T) {
	t.Parallel()

	sourceNS := "namespace1"
	sourcePVC := "pvc1"
	sourcePod := "pod1"
	sourceNode := "node1"
	sourceModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	destNS := "namespace2"
	destPvc := "pvc2"
	destPod := "pod2"
	destNode := "node2"
	destModes := []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

	pvcA := buildTestPVC(sourceNS, sourcePVC, sourceModes...)
	pvcB := buildTestPVC(destNS, destPvc, destModes...)
	podA := buildTestPod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := buildTestPod(destNS, destPod, destNode, destPvc)
	c := buildTestClient(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)

	mig := migration.Migration{
		SourceInfo: src,
		DestInfo:   dst,
	}

	s := Svc{}
	canDo := s.canDo(&mig)
	assert.True(t, canDo)
}
