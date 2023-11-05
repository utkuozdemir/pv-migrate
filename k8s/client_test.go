package k8s

import (
	_ "embed"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/_kubeconfig_test.yaml
var kubeconfig string

func TestGetClusterClient(t *testing.T) {
	t.Parallel()

	c := prepareKubeconfig()
	defer func() { _ = os.Remove(c) }()

	clusterClient, err := GetClusterClient(c, "context-1")

	require.NoError(t, err)

	rcGetter := clusterClient.RESTClientGetter

	ns, _, err := rcGetter.ToRawKubeConfigLoader().Namespace()
	require.NoError(t, err)
	assert.Equal(t, "namespace1", ns)

	discoveryClient, err := rcGetter.ToDiscoveryClient()
	require.NoError(t, err)
	assert.NotNil(t, discoveryClient)

	restConfig, err := rcGetter.ToRESTConfig()
	require.NoError(t, err)
	assert.NotNil(t, restConfig)

	restMapper, err := rcGetter.ToRESTMapper()
	require.NoError(t, err)
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
	require.NoError(t, err)
	config, _, namespace, err = buildK8sConfig(conf, "context-2")
	require.NoError(t, err)
	assert.Equal(t, "namespace2", namespace)
	assert.NotNil(t, config)
	config, _, namespace, err = buildK8sConfig(conf, "context-nonexistent")
	assert.Nil(t, config)
	assert.Equal(t, "", namespace)
	require.Error(t, err)
}

func prepareKubeconfig() string {
	testConfig, _ := os.CreateTemp("", "pv-migrate-testconfig-*.yaml")
	_, _ = testConfig.WriteString(kubeconfig)

	return testConfig.Name()
}
