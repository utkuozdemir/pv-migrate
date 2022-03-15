package k8s

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ClusterClient struct {
	RestConfig       *rest.Config
	KubeClient       kubernetes.Interface
	RESTClientGetter genericclioptions.RESTClientGetter
	NsInContext      string
}

func GetClusterClient(kubeconfigPath string, context string) (*ClusterClient, error) {
	config, rcGetter, ns, err := buildK8sConfig(kubeconfigPath, context)
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &ClusterClient{
		RestConfig:       config,
		KubeClient:       kubeClient,
		RESTClientGetter: rcGetter,
		NsInContext:      ns,
	}, nil
}

func buildK8sConfig(kubeconfigPath string, context string) (*rest.Config,
	genericclioptions.RESTClientGetter, string, error,
) {
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
		return nil, nil, "", err
	}

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, nil, "", err
	}

	rcGetter := NewRESTClientGetter(clientConfig, config)
	return clientConfig, rcGetter, namespace, nil
}
