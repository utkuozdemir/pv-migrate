package k8s

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	"testing"
)

func TestBuildK8sConfig(t *testing.T) {
	kubeconfig := test.PrepareKubeconfig()
	config, err := buildK8sConfig(kubeconfig, "")
	assert.Nil(t, err)
	assert.NotNil(t, config)
	config, err = buildK8sConfig(kubeconfig, "context-2")
	assert.Nil(t, err)
	assert.NotNil(t, config)
	config, err = buildK8sConfig(kubeconfig, "context-nonexistent")
	assert.NotNil(t, err)
	assert.Nil(t, config)
}
