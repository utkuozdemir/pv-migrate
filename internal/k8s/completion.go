package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetContexts(kubeconfigPath string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, "")
	if err != nil {
		return nil, err
	}

	rawConfig, err := client.RESTClientGetter.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return nil, err
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

func GetNamespaces(kubeconfigPath string, ctx string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, ctx)
	if err != nil {
		return nil, err
	}

	nss, err := client.KubeClient.CoreV1().
		Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	nsNames := make([]string, len(nss.Items))
	for i, ns := range nss.Items {
		nsNames[i] = ns.Name
	}

	return nsNames, nil
}

func GetPVCs(kubeconfigPath string, ctx string, namespace string) ([]string, error) {
	client, err := GetClusterClient(kubeconfigPath, ctx)
	if err != nil {
		return nil, err
	}

	pvcs, err := client.KubeClient.CoreV1().
		PersistentVolumeClaims(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	pvcNames := make([]string, len(pvcs.Items))
	for i, pvc := range pvcs.Items {
		pvcNames[i] = pvc.Name
	}

	return pvcNames, nil
}
