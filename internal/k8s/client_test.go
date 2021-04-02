package k8s

import (
     _ "embed"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"testing"
)

//go:embed test-kubeconfig.yaml
var testKubeconfig string

func TestBuildK8sConfig(t *testing.T) {
	config, err := buildK8sConfig("", "")
	assert.Nil(t, err)
	assert.NotNil(t, config)

	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(testKubeconfig)
	defer func() {
		_ = os.Remove(testConfig.Name())
	}()

	config, err = buildK8sConfig(testConfig.Name(), "")
	assert.Nil(t, err)
	assert.NotNil(t, config)
	config, err = buildK8sConfig(testConfig.Name(), "context-2")
	assert.Nil(t, err)
	assert.NotNil(t, config)
	config, err = buildK8sConfig(testConfig.Name(), "context-nonexistent")
	assert.NotNil(t, err)
	assert.Nil(t, config)
}
