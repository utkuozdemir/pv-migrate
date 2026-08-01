package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func waiting(name, reason, message string) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:  name,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}},
	}
}

func TestDescribePodWaiting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []corev1.ContainerStatus
		want     string
	}{
		{
			name:     "starting up is not worth announcing",
			statuses: []corev1.ContainerStatus{waiting("rsync", "ContainerCreating", "")},
		},
		{
			name:     "initializing is not worth announcing either",
			statuses: []corev1.ContainerStatus{waiting("rsync", "PodInitializing", "")},
		},
		{
			name:     "a real problem is announced with its message",
			statuses: []corev1.ContainerStatus{waiting("rsync", "ImagePullBackOff", "back-off pulling image")},
			want:     "rsync: ImagePullBackOff: back-off pulling image",
		},
		{
			name: "a starting container does not hide a failing one",
			statuses: []corev1.ContainerStatus{
				waiting("sidecar", "ContainerCreating", ""),
				waiting("rsync", "ErrImagePull", "not found"),
			},
			want: "rsync: ErrImagePull: not found",
		},
		{
			name:     "a running container has nothing to say",
			statuses: []corev1.ContainerStatus{{Name: "rsync", State: corev1.ContainerState{Running: nil}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: tt.statuses}}

			assert.Equal(t, tt.want, describePodWaiting(pod))
		})
	}
}
