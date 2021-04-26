package integrationtest

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func buildKubeClient(kubeconfig string) (kubernetes.Interface, *rest.Config, error) {
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		clientConfigLoadingRules.ExplicitPath = kubeconfig
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{})

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	c, err := kubernetes.NewForConfig(clientConfig)
	return c, clientConfig, err
}
