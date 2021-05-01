package engine

import (
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/testutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestBuildJob(t *testing.T) {
	testEngine := testEngine()
	testRequest := testRequest()
	migrationJob, err := testEngine.BuildJob(testRequest)
	migrationTask := task.New(migrationJob)
	assert.Nil(t, err)

	assert.Len(t, migrationTask.ID(), 5)

	assert.True(t, migrationJob.Options().DeleteExtraneousFiles())
	assert.Equal(t, "namespace1", migrationJob.Source().Claim().Namespace)
	assert.Equal(t, "pvc1", migrationJob.Source().Claim().Name)
	assert.Equal(t, "node1", migrationJob.Source().MountedNode())
	assert.False(t, migrationJob.Source().SupportsRWO())
	assert.True(t, migrationJob.Source().SupportsROX())
	assert.False(t, migrationJob.Source().SupportsRWX())
	assert.Equal(t, "namespace2", migrationJob.Dest().Claim().Namespace)
	assert.Equal(t, "pvc2", migrationJob.Dest().Claim().Name)
	assert.Equal(t, "node2", migrationJob.Dest().MountedNode())
	assert.True(t, migrationJob.Dest().SupportsRWO())
	assert.False(t, migrationJob.Dest().SupportsROX())
	assert.True(t, migrationJob.Dest().SupportsRWX())
}

func TestBuildJobMounted(t *testing.T) {
	testEngine := testEngine()
	testRequest := testRequestWithOptions(request.NewOptions(true, false, false))
	j, err := testEngine.BuildJob(testRequest)
	assert.Nil(t, j)
	assert.Error(t, err)
}

func testRequest(strategies ...string) request.Request {
	options := request.NewOptions(true, true, false)
	return testRequestWithOptions(options, strategies...)
}

func testRequestWithOptions(options request.Options, strategies ...string) request.Request {
	source := request.NewPVC("/kubeconfig1", "context1", "namespace1", "pvc1")
	dest := request.NewPVC("/kubeconfig2", "context2", "namespace2", "pvc2")
	newRequest := request.NewWithDefaultImages(source, dest, options, strategies)
	return newRequest
}

func testEngine() Engine {
	pvcA := testutil.PVCWithAccessModes("namespace1", "pvc1", v1.ReadOnlyMany)
	pvcB := testutil.PVCWithAccessModes("namespace2", "pvc2", v1.ReadWriteOnce, v1.ReadWriteMany)
	podA := testutil.Pod("namespace1", "pod1", "node1", "pvc1")
	podB := testutil.Pod("namespace2", "pod2", "node2", "pvc2")
	kubernetesClientProvider := testutil.KubernetesClientProvider{Objects: []runtime.Object{pvcA, pvcB, podA, podB}}
	e := NewWithKubernetesClientProvider(&kubernetesClientProvider)
	return e
}
