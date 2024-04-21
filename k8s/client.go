package k8s

import (
	"fmt"
	"log/slog"

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

func GetClusterClient(kubeconfigPath string, context string, logger *slog.Logger) (*ClusterClient, error) {
	config, rcGetter, namespace, err := buildK8sConfig(kubeconfigPath, context, logger)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &ClusterClient{
		RestConfig:       config,
		KubeClient:       kubeClient,
		RESTClientGetter: rcGetter,
		NsInContext:      namespace,
	}, nil
}

//nolint:ireturn,nolintlint
func buildK8sConfig(kubeconfigPath string, context string, logger *slog.Logger) (*rest.Config,
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
		return nil, nil, "", fmt.Errorf("failed to get namespace from kubeconfig: %w", err)
	}

	clientConfig, err := config.ClientConfig()
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create kubernetes client config: %w", err)
	}

	rcGetter := NewRESTClientGetter(clientConfig, config, logger)

	return clientConfig, rcGetter, namespace, nil
}
