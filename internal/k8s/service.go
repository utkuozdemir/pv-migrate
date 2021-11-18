package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"time"
)

const (
	serviceLbCheckTimeout = 120 * time.Second
)

func GetServiceAddress(cli kubernetes.Interface, ns string, name string) (string, error) {
	var result string

	resCli := cli.CoreV1().Services(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	ctx, cancel := context.WithTimeout(context.TODO(), serviceLbCheckTimeout)
	defer cancel()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector
			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector
			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, lw, &corev1.Service{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Service)
			if !ok {
				return false, fmt.Errorf("unexpected type while watcing service %s/%s", ns, name)
			}

			if res.Spec.Type == corev1.ServiceTypeClusterIP {
				result = res.Name + "." + res.Namespace
				return true, nil
			}

			if len(res.Status.LoadBalancer.Ingress) > 0 {
				result = res.Status.LoadBalancer.Ingress[0].IP
				return true, nil
			}

			return false, nil
		})
	return result, err
}
