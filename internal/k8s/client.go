package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetClientAndNsInContext(kubeconfigPath string, context string) (kubernetes.Interface, string, error) {
	config, namespace, err := buildK8sConfig(kubeconfigPath, context)
	if err != nil {
		return nil, "", err
	}
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", err
	}

	return kubeClient, namespace, err
}

func buildK8sConfig(kubeconfigPath string, context string) (*rest.Config, string, error) {
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		clientConfigLoadingRules.ExplicitPath = kubeconfigPath
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		})

	namespace, _, err := config.Namespace()
	if err != nil {
		return nil, "", err
	}

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, "", err
	}

	return clientConfig, namespace, nil
}
