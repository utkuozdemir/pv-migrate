package k8s_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

//nolint:funlen
func TestFindJobPod(t *testing.T) {
	t.Parallel()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync",
			Namespace: "default",
		},
	}

	tests := []struct {
		name       string
		pods       []corev1.Pod
		wantPod    string
		wantErrMsg string
	}{
		{
			name:       "no pods",
			pods:       nil,
			wantErrMsg: "no pods found for job test-rsync",
		},
		{
			name: "single running pod",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rsync-abc",
						Namespace: "default",
						Labels:    map[string]string{"job-name": "test-rsync"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			wantPod: "test-rsync-abc",
		},
		{
			name: "prefers running over pending",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rsync-pending",
						Namespace: "default",
						Labels:    map[string]string{"job-name": "test-rsync"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rsync-running",
						Namespace: "default",
						Labels:    map[string]string{"job-name": "test-rsync"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			wantPod: "test-rsync-running",
		},
		{
			name: "falls back to first pod when none running",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rsync-succeeded",
						Namespace: "default",
						Labels:    map[string]string{"job-name": "test-rsync"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
				},
			},
			wantPod: "test-rsync-succeeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			cli := fake.NewClientset()
			for i := range tt.pods {
				_, err := cli.CoreV1().Pods("default").Create(ctx, &tt.pods[i], metav1.CreateOptions{})
				require.NoError(t, err)
			}

			pod, err := k8s.FindJobPod(ctx, cli, job)
			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPod, pod.Name)
		})
	}
}

//nolint:funlen
func TestFindDataMoverJob(t *testing.T) {
	t.Parallel()

	helmLabels := map[string]string{"app.kubernetes.io/managed-by": "Helm"}

	tests := []struct {
		name       string
		jobs       []batchv1.Job
		ns         string
		prefix     string
		wantJob    string
		wantNs     string
		wantErrMsg string
	}{
		{
			name:       "no jobs",
			ns:         "default",
			prefix:     "pv-migrate-abc12",
			wantErrMsg: "no job found for migration pv-migrate-abc12",
		},
		{
			name: "single-release rsync job",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-abc12-rsync",
						Namespace: "default",
						Labels:    helmLabels,
					},
				},
			},
			ns:      "default",
			prefix:  "pv-migrate-abc12",
			wantJob: "pv-migrate-abc12-rsync",
			wantNs:  "default",
		},
		{
			name: "dual-release dest rsync job",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-abc12-dest-rsync",
						Namespace: "ns1",
						Labels:    helmLabels,
					},
				},
			},
			ns:      "ns1",
			prefix:  "pv-migrate-abc12",
			wantJob: "pv-migrate-abc12-dest-rsync",
			wantNs:  "ns1",
		},
		{
			name: "with strategy suffix",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-fuzzy-panda-clusterip-rsync",
						Namespace: "default",
						Labels:    helmLabels,
					},
				},
			},
			ns:      "default",
			prefix:  "pv-migrate-fuzzy-panda-",
			wantJob: "pv-migrate-fuzzy-panda-clusterip-rsync",
			wantNs:  "default",
		},
		{
			name: "does not match prefix collision",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-foo2-clusterip-rsync",
						Namespace: "default",
						Labels:    helmLabels,
					},
				},
			},
			ns:         "default",
			prefix:     "pv-migrate-foo-",
			wantErrMsg: "no job found",
		},
		{
			name: "falls back to all namespaces",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-abc12-rsync",
						Namespace: "other-ns",
						Labels:    helmLabels,
					},
				},
			},
			ns:      "wrong-ns",
			prefix:  "pv-migrate-abc12",
			wantJob: "pv-migrate-abc12-rsync",
			wantNs:  "other-ns",
		},
		{
			name: "ignores non-rsync jobs",
			jobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pv-migrate-abc12-sshd",
						Namespace: "default",
						Labels:    helmLabels,
					},
				},
			},
			ns:         "",
			prefix:     "pv-migrate-abc12",
			wantErrMsg: "no job found for migration pv-migrate-abc12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			cli := fake.NewClientset()
			for i := range tt.jobs {
				_, err := cli.BatchV1().Jobs(tt.jobs[i].Namespace).Create(ctx, &tt.jobs[i], metav1.CreateOptions{})
				require.NoError(t, err)
			}

			job, err := k8s.FindDataMoverJob(ctx, cli, tt.ns, tt.prefix)
			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantJob, job.Name)
			assert.Equal(t, tt.wantNs, job.Namespace)
		})
	}
}
