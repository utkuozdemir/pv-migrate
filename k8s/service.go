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
	ctx context.Context,
	cli kubernetes.Interface,
	namespace string,
	name string,
	lbTimeout time.Duration,
) (string, error) {
	var result string

	resCli := cli.CoreV1().Services(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(ctx, lbTimeout)
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

// GetNodePortServiceDetails gets the IP of a worker node and the assigned NodePort for the service.
//
// It returns the IP of a worker node running the service and the assigned NodePort.
func GetNodePortServiceDetails(
	ctx context.Context,
	cli kubernetes.Interface,
	namespace string,
	name string,
	timeout time.Duration,
) (string, int, error) {
	resCli := cli.CoreV1().Services(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var nodeIP string
	var nodePort int

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
			svc, ok := event.Object.(*corev1.Service)
			if !ok {
				return false, fmt.Errorf("unexpected type while watching service: %s/%s", namespace, name)
			}

			if svc.Spec.Type != corev1.ServiceTypeNodePort {
				return false, fmt.Errorf("service %s/%s is not of type NodePort", namespace, name)
			}

			// Get the NodePort from the service
			if len(svc.Spec.Ports) > 0 {
				for _, port := range svc.Spec.Ports {
					if port.Name == "ssh" || port.Port == 22 {
						nodePort = int(port.NodePort)
						break
					}
				}
				if nodePort == 0 && len(svc.Spec.Ports) > 0 {
					// If no SSH port found, just use the first port
					nodePort = int(svc.Spec.Ports[0].NodePort)
				}
			}

			// Get a worker node IP
			nodes, err := cli.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, fmt.Errorf("failed to list nodes: %w", err)
			}

			for _, node := range nodes.Items {
				for _, addr := range node.Status.Addresses {
					if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
						nodeIP = addr.Address
						break
					}
				}
				if nodeIP != "" {
					break
				}
			}

			return nodeIP != "" && nodePort != 0, nil
		}); err != nil {
		return "", 0, fmt.Errorf("failed to get NodePort service %s/%s details: %w", namespace, name, err)
	}

	if nodeIP == "" || nodePort == 0 {
		return "", 0, fmt.Errorf("failed to get node IP or NodePort for service %s/%s", namespace, name)
	}

	return nodeIP, nodePort, nil
}
