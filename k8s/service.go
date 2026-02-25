package k8s

import (
	"context"
	"errors"
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
	ns, name string,
	lbTimeout time.Duration,
) (string, error) {
	var result string

	resCli := cli.CoreV1().Services(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(ctx, lbTimeout)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list services %s/%s: %w", ns, name, err)
			}

			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			resWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch services %s/%s: %w", ns, name, err)
			}

			return resWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Service{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Service)
			if !ok {
				return false, fmt.Errorf("unexpected type while watching service: %s/%s", ns, name)
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
		return "", fmt.Errorf("failed to get service %s/%s address: %w", ns, name, err)
	}

	return result, nil
}

// GetNodePort waits for a NodePort service to be ready and returns its assigned port.
func GetNodePort(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, name string,
	timeout time.Duration,
) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	svc, err := waitForNodePortService(ctx, cli, ns, name)
	if err != nil {
		return 0, err
	}

	return findNodePort(svc)
}

// GetNodeIP returns a usable IP address for the named node.
func GetNodeIP(ctx context.Context, cli kubernetes.Interface, nodeName string) (string, error) {
	node, err := cli.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
			return addr.Address, nil
		}
	}

	return "", fmt.Errorf("node %s has no usable IP address", nodeName)
}

// GetAnyNodeIP returns a usable IP address from any node in the cluster.
func GetAnyNodeIP(ctx context.Context, cli kubernetes.Interface) (string, error) {
	nodes, err := cli.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
				return addr.Address, nil
			}
		}
	}

	return "", errors.New("no node with a usable IP address found")
}

// waitForNodePortService waits for a NodePort service to be ready.
func waitForNodePortService(ctx context.Context, cli kubernetes.Interface, ns, name string) (*corev1.Service, error) {
	resCli := cli.CoreV1().Services(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	var resultSvc *corev1.Service

	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list services %s/%s: %w", ns, name, err)
			}

			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			resWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch services %s/%s: %w", ns, name, err)
			}

			return resWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Service{}, nil,
		func(event watch.Event) (bool, error) {
			svc, ok := event.Object.(*corev1.Service)
			if !ok {
				return false, fmt.Errorf("unexpected type while watching service: %s/%s", ns, name)
			}

			if svc.Spec.Type != corev1.ServiceTypeNodePort {
				return false, fmt.Errorf("service %s/%s is not of type NodePort", ns, name)
			}

			resultSvc = svc

			return true, nil
		}); err != nil {
		return nil, fmt.Errorf("failed to get NodePort service %s/%s details: %w", ns, name, err)
	}

	return resultSvc, nil
}

// findNodePort extracts the NodePort from a service.
func findNodePort(svc *corev1.Service) (int, error) {
	if len(svc.Spec.Ports) == 0 {
		return 0, errors.New("service has no ports defined")
	}

	// First try to find SSH port
	for _, port := range svc.Spec.Ports {
		if port.Name == "ssh" || port.Port == 22 {
			return int(port.NodePort), nil
		}
	}

	// Fallback to the first port
	return int(svc.Spec.Ports[0].NodePort), nil
}
