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
	restConfig      *rest.Config
	clientConfig    clientcmd.ClientConfig
	discoveryClient discovery.CachedDiscoveryInterface
	restMapper      meta.RESTMapper
}

func NewRESTClientGetter(restConfig *rest.Config, clientConfig clientcmd.ClientConfig) *HelmRESTClientGetter {
	discoveryClient := buildDiscoveryClient(restConfig)
	restMapper := buildRESTMapper(discoveryClient)

	return &HelmRESTClientGetter{
		restConfig:      restConfig,
		clientConfig:    clientConfig,
		discoveryClient: discoveryClient,
		restMapper:      restMapper,
	}
}

func (c *HelmRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return c.restConfig, nil
}

func (c *HelmRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return c.discoveryClient, nil
}

func (c *HelmRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return c.restMapper, nil
}

func (c *HelmRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return c.clientConfig
}

func buildDiscoveryClient(config *rest.Config) discovery.CachedDiscoveryInterface {
	discoveryClient, _ := discovery.NewDiscoveryClientForConfig(config)

	return memory.NewMemCacheClient(discoveryClient)
}

func buildRESTMapper(discoveryClient discovery.CachedDiscoveryInterface) meta.RESTMapper {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)

	return restmapper.NewShortcutExpander(mapper, discoveryClient)
}
