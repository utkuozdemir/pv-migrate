package k8s

import (
	"context"
	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CleanupForID removes the kubernetes resources for the given instance id using label selectors.
// It removes services, jobs and pods that belong to the given instance.
func CleanupForID(kubeClient kubernetes.Interface, namespace string, id string) error {
	pods := kubeClient.CoreV1().Pods(namespace)
	jobs := kubeClient.BatchV1().Jobs(namespace)
	services := kubeClient.CoreV1().Services(namespace)
	deleteOptions := metav1.DeleteOptions{}
	listOptions := metav1.ListOptions{
		LabelSelector: LabelSelector(id),
	}

	var result *multierror.Error
	err := jobs.DeleteCollection(context.TODO(), deleteOptions, listOptions)
	if err != nil {
		result = multierror.Append(result, err)
	}

	err = pods.DeleteCollection(context.TODO(), deleteOptions, listOptions)
	if err != nil {
		result = multierror.Append(result, err)
	}

	serviceList, err := services.List(context.TODO(), listOptions)
	if err != nil {
		result = multierror.Append(result, err)
	}

	for _, service := range serviceList.Items {
		err = services.Delete(context.TODO(), service.Name, deleteOptions)
		if err != nil {
			result = multierror.Append(result, err)
		}
	}

	//goland:noinspection GoNilness
	return result.ErrorOrNil()
}
