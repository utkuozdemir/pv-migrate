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

// GetNodePort waits for a Service that carries a node port, of type NodePort or
// LoadBalancer, and returns the port allocated for ssh.
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

// waitForNodePortService waits for a Service that carries a node port.
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

			if svc.Spec.Type != corev1.ServiceTypeNodePort && svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
				return false, fmt.Errorf("service %s/%s is of type %s and has no node port", ns, name, svc.Spec.Type)
			}

			resultSvc = svc

			return true, nil
		}); err != nil {
		return nil, fmt.Errorf("failed to get NodePort service %s/%s details: %w", ns, name, err)
	}

	return resultSvc, nil
}

// findNodePort extracts the node port for ssh from a Service, or the first
// port's. A LoadBalancer Service can be created without node ports, and then
// there is nothing to connect to.
func findNodePort(svc *corev1.Service) (int, error) {
	if len(svc.Spec.Ports) == 0 {
		return 0, errors.New("service has no ports defined")
	}

	port := svc.Spec.Ports[0]

	for _, candidate := range svc.Spec.Ports {
		if candidate.Name == "ssh" || candidate.Port == 22 {
			port = candidate

			break
		}
	}

	if port.NodePort == 0 {
		return 0, fmt.Errorf("service %s/%s has no node port allocated", svc.Namespace, svc.Name)
	}

	return int(port.NodePort), nil
}
