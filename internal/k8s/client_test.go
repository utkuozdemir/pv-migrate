package k8s

import (
	_ "embed"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed _kubeconfig_test.yaml
var kubeconfig string

func TestGetClusterClient(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	conf := prepareKubeconfig()
	defer func() {
		_ = os.Remove(conf)
	}()

	config, _, namespace, err := buildK8sConfig(conf, "")
	assert.NotNil(t, config)
	assert.Equal(t, "namespace1", namespace)
	assert.Nil(t, err)
	config, _, namespace, err = buildK8sConfig(conf, "context-2")
	assert.Nil(t, err)
	assert.Equal(t, "namespace2", namespace)
	assert.NotNil(t, config)
	config, _, namespace, err = buildK8sConfig(conf, "context-nonexistent")
	assert.Nil(t, config)
	assert.Equal(t, "", namespace)
	assert.NotNil(t, err)
}

func prepareKubeconfig() string {
	testConfig, _ := ioutil.TempFile("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)

	return testConfig.Name()
}
