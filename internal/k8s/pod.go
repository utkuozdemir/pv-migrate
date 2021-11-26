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
	podWatchTimeout = 2 * time.Minute
)

func WaitForPod(cli kubernetes.Interface, ns, labelSelector string) (*corev1.Pod, error) {
	var result *corev1.Pod

	resCli := cli.CoreV1().Pods(ns)
	ctx, cancel := context.WithTimeout(context.TODO(), podWatchTimeout)
	defer cancel()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector
			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector
			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, lw, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected type while watcing pods "+
					"in ns %s with label selector %s", ns, labelSelector)
			}

			phase := res.Status.Phase
			if phase != corev1.PodPending {
				result = res
				return true, nil
			}

			return false, nil
		})

	return result, err
}

func waitForPodTermination(cli kubernetes.Interface, ns string, name string) (*corev1.PodPhase, error) {
	var result *corev1.PodPhase

	resCli := cli.CoreV1().Pods(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	ctx, cancel := context.WithTimeout(context.TODO(), podWatchTimeout)
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

	_, err := watchtools.UntilWithSync(ctx, lw, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected type while watcing pod %s/%s", ns, name)
			}

			phase := res.Status.Phase
			if phase != corev1.PodRunning {
				result = &phase
				return true, nil
			}

			return false, nil
		})

	return result, err
}
