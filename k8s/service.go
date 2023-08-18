package k8s

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

//nolint:funlen
func GetServiceAddress(
	cli kubernetes.Interface,
	namespace string,
	name string,
	lbTimeout time.Duration,
) (string, error) {
	var result string

	resCli := cli.CoreV1().Services(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(context.TODO(), lbTimeout)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list services %s/%s: %w", namespace, name, err)
			}

			return list, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			resWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch services %s/%s: %w", namespace, name, err)
			}

			return resWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Service{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Service)
			if !ok {
				return false, fmt.Errorf("unexpected type while watching service: %s/%s", namespace, name)
			}

			if res.Spec.Type == corev1.ServiceTypeClusterIP {
				result = res.Name + "." + res.Namespace

				return true, nil
			}

			if len(res.Status.LoadBalancer.Ingress) > 0 {
				if len(res.Status.LoadBalancer.Ingress[0].Hostname) > 0 {
					result = res.Status.LoadBalancer.Ingress[0].Hostname
				} else {
					result = res.Status.LoadBalancer.Ingress[0].IP
				}

				return true, nil
			}

			return false, nil
		}); err != nil {
		return "", fmt.Errorf("failed to get service %s/%s address: %w", namespace, name, err)
	}

	return result, nil
}
