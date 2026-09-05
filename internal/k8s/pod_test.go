package k8s_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

// Not parallel: the feature gate is process-wide. The streaming list the
// reflector prefers is not served by the fake clientset.
//
//nolint:paralleltest
func TestWaitForPodReady(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	pod := func(ready corev1.ConditionStatus) *corev1.Pod {
		return &corev1.Pod{
			Namespace: "ns", Name: "sshd-1", Labels: map[string]string{"app": "sshd"},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}},
			},
		}
	}

	got, err := k8s.WaitForPodReady(t.Context(), fake.NewClientset(pod(corev1.ConditionTrue)), "ns", "app=sshd",
		time.Minute, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, "sshd-1", got.Name)

	_, err = k8s.WaitForPodReady(t.Context(), fake.NewClientset(pod(corev1.ConditionFalse)), "ns", "app=sshd",
		20*time.Millisecond, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out after 20ms",
		"a pod that is Running but not Ready is not done, and the error names the budget")
}
