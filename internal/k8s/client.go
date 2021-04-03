package k8s

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesClientProvider provides a kubernetes.Interface instance for the given kubeconfig path and the context.
type KubernetesClientProvider interface {
	GetKubernetesClient(kubeconfigPath string, context string) (kubernetes.Interface, error)
}

type kubernetesClientProvider struct {
}

func (k *kubernetesClientProvider) GetKubernetesClient(kubeconfigPath string, context string) (kubernetes.Interface, error) {
	config, err := buildK8sConfig(kubeconfigPath, context)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// NewKubernetesClientProvider creates a new KubernetesClientProvider that provides "real" kubernetes api clients.
func NewKubernetesClientProvider() KubernetesClientProvider {
	return &kubernetesClientProvider{}
}

func buildK8sConfig(kubeconfigPath string, context string) (*rest.Config, error) {
	clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		clientConfigLoadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}
