package k8s

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	"testing"
)

func TestBuildK8sConfig(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	config, ns, err := buildK8sConfig(kubeconfig, "")
	assert.NotNil(t, config)
	assert.Equal(t, "namespace1", ns)
	assert.Nil(t, err)
	config, ns, err = buildK8sConfig(kubeconfig, "context-2")
	assert.Nil(t, err)
	assert.Equal(t, "namespace2", ns)
	assert.NotNil(t, config)
	config, ns, err = buildK8sConfig(kubeconfig, "context-nonexistent")
	assert.Nil(t, config)
	assert.Equal(t, "", ns)
	assert.NotNil(t, err)
}
