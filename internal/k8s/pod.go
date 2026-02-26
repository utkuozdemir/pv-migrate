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

const (
	podWatchTimeout = 2 * time.Minute
)

func WaitForPod(ctx context.Context, cli kubernetes.Interface, ns, labelSelector string) (*corev1.Pod, error) {
	var result *corev1.Pod

	resCli := cli.CoreV1().Pods(ns)

	ctx, cancel := context.WithTimeout(ctx, podWatchTimeout)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector

			resWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch pods: %w", err)
			}

			return resWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf(
					"unexpected type while watching pods: ns: %s, labelSelector: %s",
					ns,
					labelSelector,
				)
			}

			phase := res.Status.Phase
			if phase != corev1.PodPending {
				result = res

				return true, nil
			}

			return false, nil
		}); err != nil {
		return nil, fmt.Errorf("failed to wait for pod: %w", err)
	}

	return result, nil
}

func waitForPodTermination(ctx context.Context, cli kubernetes.Interface, ns, name string) (*corev1.PodPhase, error) {
	var result *corev1.PodPhase

	resCli := cli.CoreV1().Pods(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			resWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch pods: %w", err)
			}

			return resWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected type while watching pods: %s/%s", ns, name)
			}

			phase := res.Status.Phase
			if phase != corev1.PodRunning {
				result = &phase

				return true, nil
			}

			return false, nil
		}); err != nil {
		return nil, fmt.Errorf("failed to wait for pod termination: %w", err)
	}

	return result, nil
}
