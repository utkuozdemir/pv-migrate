package k8s

import (
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetK8sClient(kubeconfigPath string, context string) (kubernetes.Interface, error) {
	config, err := buildK8sConfig(kubeconfigPath, context)
	sourceKubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithError(err).Error("Error building kubernetes client")
		return nil, err
	}
	return sourceKubeClient, nil
}

func buildK8sConfig(kubeconfigPath string, context string) (*rest.Config, error) {
	clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfigLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfigPath != "" {
		clientConfigLoadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientConfigLoadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
	if err != nil {
		log.WithError(err).Error("Error building kubernetes config")
		return nil, err
	}
	return config, err
}
