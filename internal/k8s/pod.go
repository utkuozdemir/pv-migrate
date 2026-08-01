package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

const (
	podWatchTimeout = 2 * time.Minute
)

func WaitForPod(
	ctx context.Context, cli kubernetes.Interface, ns, labelSelector string, logger *slog.Logger,
) (*corev1.Pod, error) {
	var result *corev1.Pod

	// The watch already delivers every pod update, so narrating a container's
	// waiting reason turns minutes of silence into an answer, using the same
	// observation the failure block would print later.
	lastWaiting := ""

	resCli := cli.CoreV1().Pods(ns)

	ctx, cancel := context.WithTimeout(ctx, podWatchTimeout)
	defer cancel()

	listWatch := podLabelListWatch(resCli, labelSelector)

	condition := func(event watch.Event) (bool, error) {
		res, ok := event.Object.(*corev1.Pod)
		if !ok {
			return false, fmt.Errorf(
				"unexpected type while watching pods: ns: %s, labelSelector: %s",
				ns,
				labelSelector,
			)
		}

		if waiting := describePodWaiting(res); waiting != "" && waiting != lastWaiting && logger != nil {
			lastWaiting = waiting

			logger.Warn("🔶 Pod is not starting yet", "pod", res.Name, "waiting", waiting)
		}

		if res.Status.Phase != corev1.PodPending {
			result = res

			return true, nil
		}

		return false, nil
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil, condition); err != nil {
		// The bare watch error says "timed out waiting for the condition" and
		// names neither the wait nor its budget.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("timed out after %s waiting for a pod (selector %s) to start: %w",
				podWatchTimeout, labelSelector, err)
		}

		return nil, fmt.Errorf("failed to wait for pod: %w", err)
	}

	return result, nil
}

// podLabelListWatch lists and watches pods by label selector.
func podLabelListWatch(resCli clientcorev1.PodInterface, labelSelector string) *cache.ListWatch {
	return &cache.ListWatch{
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
}

// benignWaitingReasons are what a container reports while it is starting up.
// Every healthy run reports one of them for a second or two, so announcing them
// makes an ordinary migration look like it is in trouble. What is left over all
// means something has actually gone wrong, and that is worth saying as soon as
// it is known.
var benignWaitingReasons = map[string]bool{
	"ContainerCreating": true,
	"PodInitializing":   true,
}

// describePodWaiting summarizes why a pending pod's containers are waiting, or
// returns an empty string when there is nothing to say yet.
func describePodWaiting(pod *corev1.Pod) string {
	var parts []string

	for _, status := range pod.Status.ContainerStatuses {
		waiting := status.State.Waiting
		if waiting == nil || waiting.Reason == "" || benignWaitingReasons[waiting.Reason] {
			continue
		}

		parts = append(parts, status.Name+": "+describeWaiting(waiting))
	}

	return strings.Join(parts, ", ")
}

// waitForPodTermination returns the pod as it looked when it left the Running
// phase. The whole object rather than the phase, because whoever decided the pod
// failed also needs the container's exit code from that same observation.
func waitForPodTermination(ctx context.Context, cli kubernetes.Interface, ns, name string) (*corev1.Pod, error) {
	var result *corev1.Pod

	resCli := cli.CoreV1().Pods(ns)

	pod, err := resCli.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	if pod.Status.Phase != corev1.PodRunning {
		return pod, nil
	}

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

			if res.Status.Phase != corev1.PodRunning {
				result = res

				return true, nil
			}

			return false, nil
		}); err != nil {
		return nil, fmt.Errorf("failed to wait for pod termination: %w", err)
	}

	return result, nil
}
