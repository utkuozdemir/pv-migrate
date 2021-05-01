package strategy

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/testutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestSvcCanDoSameCluster(t *testing.T) {
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

	pvcA := testutil.PVCWithAccessModes(sourceNS, sourcePVC, sourceModes...)
	pvcB := testutil.PVCWithAccessModes(destNS, destPvc, destModes...)
	podA := testutil.Pod(sourceNS, sourcePod, sourceNode, sourcePVC)
	podB := testutil.Pod(destNS, destPod, destNode, destPvc)
	c := fake.NewSimpleClientset(pvcA, pvcB, podA, podB)
	src, _ := pvc.New(c, sourceNS, sourcePVC)
	dst, _ := pvc.New(c, destNS, destPvc)
	j := job.New(src, dst, defaultJobOptions, "", "")

	s := Svc{}
	canDo := s.canDo(j)
	assert.True(t, canDo)
}
