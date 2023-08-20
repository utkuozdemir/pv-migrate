package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetContexts(kubeconfigPath string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, "")
	if err != nil {
		return nil, err
	}

	rawConfig, err := client.RESTClientGetter.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	ctxs := rawConfig.Contexts

	contextNames := make([]string, len(ctxs))

	index := 0

	for name := range ctxs {
		contextNames[index] = name
		index++
	}

	return contextNames, nil
}

func GetNamespaces(ctx context.Context, kubeconfigPath string, kubectx string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, kubectx)
	if err != nil {
		return nil, err
	}

	nss, err := client.KubeClient.CoreV1().
		Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	nsNames := make([]string, len(nss.Items))
	for i, ns := range nss.Items {
		nsNames[i] = ns.Name
	}

	return nsNames, nil
}

func GetPVCs(ctx context.Context, kubeconfigPath string, kubectx string, namespace string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, kubectx)
	if err != nil {
		return nil, err
	}

	pvcs, err := client.KubeClient.CoreV1().
		PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PVCs: %w", err)
	}

	pvcNames := make([]string, len(pvcs.Items))
	for i, pvc := range pvcs.Items {
		pvcNames[i] = pvc.Name
	}

	return pvcNames, nil
}
