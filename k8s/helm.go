package k8s

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type HelmRESTClientGetter struct {
	restConfig   *rest.Config
	clientConfig clientcmd.ClientConfig
}

func NewRESTClientGetter(restConfig *rest.Config,
	clientConfig clientcmd.ClientConfig,
) *HelmRESTClientGetter {
	return &HelmRESTClientGetter{
		restConfig:   restConfig,
		clientConfig: clientConfig,
	}
}

func (c *HelmRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.restConfig, nil
}

//nolint:ireturn,nolintlint
func (c *HelmRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	config, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	config.Burst = 100
	discoveryClient, _ := discovery.NewDiscoveryClientForConfig(config)

	return memory.NewMemCacheClient(discoveryClient), nil
}

//nolint:ireturn,nolintlint
func (c *HelmRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	expander := restmapper.NewShortcutExpander(mapper, discoveryClient)

	return expander, nil
}

//nolint:ireturn,nolintlint
func (c *HelmRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return c.clientConfig
}
