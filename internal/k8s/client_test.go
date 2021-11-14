package k8s

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"testing"
)

//go:embed _kubeconfig_test.yaml
var kubeconfig string

func TestGetClusterClient(t *testing.T) {
	c := prepareKubeconfig()
	defer func() { _ = os.Remove(c) }()
	clusterClient, err := GetClusterClient(c, "context-1")
	assert.NoError(t, err)

	rcGetter := clusterClient.RESTClientGetter

	ns, _, err := rcGetter.ToRawKubeConfigLoader().Namespace()
	assert.NoError(t, err)
	assert.Equal(t, "namespace1", ns)

	discoveryClient, err := rcGetter.ToDiscoveryClient()
	assert.NoError(t, err)
	assert.NotNil(t, discoveryClient)

	restConfig, err := rcGetter.ToRESTConfig()
	assert.NoError(t, err)
	assert.NotNil(t, restConfig)

	restMapper, err := rcGetter.ToRESTMapper()
	assert.NoError(t, err)
	assert.NotNil(t, restMapper)
}

func TestBuildK8sConfig(t *testing.T) {
	c := prepareKubeconfig()
	defer func() {
		_ = os.Remove(c)
	}()

	config, _, ns, err := buildK8sConfig(c, "")
	assert.NotNil(t, config)
	assert.Equal(t, "namespace1", ns)
	assert.Nil(t, err)
	config, _, ns, err = buildK8sConfig(c, "context-2")
	assert.Nil(t, err)
	assert.Equal(t, "namespace2", ns)
	assert.NotNil(t, config)
	config, _, ns, err = buildK8sConfig(c, "context-nonexistent")
	assert.Nil(t, config)
	assert.Equal(t, "", ns)
	assert.NotNil(t, err)
}

func prepareKubeconfig() string {
	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)
	return testConfig.Name()
}
