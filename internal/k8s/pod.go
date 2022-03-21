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

const (
	podWatchTimeout = 2 * time.Minute
)

var ErrUnexpectedTypePodWatch = errors.New("unexpected type while watching pods")

func WaitForPod(cli kubernetes.Interface, namespace, labelSelector string) (*corev1.Pod, error) {
	var result *corev1.Pod

	resCli := cli.CoreV1().Pods(namespace)

	ctx, cancel := context.WithTimeout(context.TODO(), podWatchTimeout)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector

			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector

			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("%w: ns: %s, labelSelector: %s",
					ErrUnexpectedTypePodWatch, namespace, labelSelector)
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

func waitForPodTermination(cli kubernetes.Interface, namespace string, name string) (*corev1.PodPhase, error) {
	var result *corev1.PodPhase

	resCli := cli.CoreV1().Pods(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	ctx := context.Background()
	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("%w: %s/%s", ErrUnexpectedTypePodWatch, namespace, name)
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
