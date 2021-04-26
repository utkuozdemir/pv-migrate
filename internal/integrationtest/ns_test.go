package integrationtest

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func ensureNamespaceExists(kubeClient kubernetes.Interface, name string) (*corev1.Namespace, error) {
	namespaces := kubeClient.CoreV1().Namespaces()
	foundNs, err := namespaces.Get(context.TODO(), name, metav1.GetOptions{})
	if err == nil {
		return foundNs, nil
	}

	if !kubeerrors.IsNotFound(err) {
		return nil, err
	}

	return namespaces.Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})
}

func ensureNamespaceIsDeleted(kubeClient kubernetes.Interface, name string) error {
	namespaces := kubeClient.CoreV1().Namespaces()
	err := namespaces.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if kubeerrors.IsNotFound(err) {
		return nil
	}
	return err
}
