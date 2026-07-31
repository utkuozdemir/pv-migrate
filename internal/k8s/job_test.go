package k8s_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/utkuozdemir/pv-migrate/internal/console"
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

func TestWaitForJobCompletion_PodAlreadySucceededDoesNotWatchTermination(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rclone-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rclone"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	})

	err := k8s.WaitForJobCompletion(ctx, cli, "default", "test-rclone", false, false, console.Palette{},
		&bytes.Buffer{}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	for _, action := range cli.Actions() {
		assert.NotEqual(t, "watch", action.GetVerb(), "already-succeeded pod should not start a watch")
	}
}

func TestWaitForJobCompletion_PodAlreadyFailedReturnsError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rclone-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rclone"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "rclone",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 3}},
				},
			},
		},
	})

	err := k8s.WaitForJobCompletion(ctx, cli, "default", "test-rclone", false, false, console.Palette{},
		&bytes.Buffer{}, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err,
		`pod default/test-rclone-abc failed: the data mover exited with code 3 `+
			`(rclone documents this exit code as: Directory not found)`)

	for _, action := range cli.Actions() {
		assert.NotEqual(t, "watch", action.GetVerb(), "already-failed pod should not start a watch")
	}
}

// TestWaitForJobCompletion_RendersTerminatedStateAdditively covers the states a
// table row cannot explain: OOMKilled next to 137 says more than any exit code
// meaning does, so nothing replaces anything else.
func TestWaitForJobCompletion_RendersTerminatedStateAdditively(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rsync"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "sidecar", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{
					Name: "rsync",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
						ExitCode: 137,
						Signal:   9,
						Reason:   "OOMKilled",
						Message:  " out of memory ",
					}},
				},
			},
		},
	})

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", false, false, console.Palette{},
		&bytes.Buffer{}, slog.New(slog.DiscardHandler))
	require.Error(t, err)

	assert.Contains(t, err.Error(), "the data mover exited with code 137")
	assert.Contains(t, err.Error(), "killed by signal 9")
	assert.Contains(t, err.Error(), "reason: OOMKilled")
	assert.Contains(t, err.Error(), "message: out of memory")
}

// TestWaitForJobCompletion_PodFailedWithoutContainerStatus is the evicted-pod
// shape: nothing terminated, so the pod's own reason is all there is to say.
func TestWaitForJobCompletion_PodFailedWithoutContainerStatus(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rsync"},
		},
		Status: corev1.PodStatus{
			Phase:   corev1.PodFailed,
			Reason:  "Evicted",
			Message: "The node was low on resource: ephemeral-storage",
		},
	})

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", false, false, console.Palette{},
		&bytes.Buffer{}, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "pod default/test-rsync-abc failed: reason: Evicted")
	assert.NotContains(t, err.Error(), "exited with code",
		"an exit code that was never reported must not be invented")
}

// TestWaitForJobCompletion_RunningPodThatFails is the path where the progress
// follower is still alive at the moment of failure. It pins both halves: the log
// tail is printed, and it is printed after the follower has been joined, so it
// cannot interleave with the progress bar's redraws on the same writer. The fake
// clientset offers no way to synchronise on the follow stream being open, so the
// transition below is timer-based and the interleaving half is best-effort here;
// the ordering itself is enforced structurally (the tail is only written after
// the errgroup is joined) and the shared-variable race is gone by construction.
//
//nolint:paralleltest // the feature gate below is process-wide
func TestWaitForJobCompletion_RunningPodThatFails(t *testing.T) {
	// Not parallel: the feature gate is process-wide. The streaming list the
	// reflector prefers is not served by the fake clientset, and this is the only
	// test here that watches anything.
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	cli := fake.NewClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rsync"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	createPod(t, cli, pod)

	writer := &syncWriter{}

	failed := pod.DeepCopy()
	failed.Status = corev1.PodStatus{
		Phase: corev1.PodFailed,
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name:  "rsync",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 23}},
			},
		},
	}

	go func() {
		time.Sleep(300 * time.Millisecond)

		_, err := cli.CoreV1().Pods("default").UpdateStatus(context.Background(), failed, metav1.UpdateOptions{})
		assert.NoError(t, err)
	}()

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", true, false, console.Palette{},
		writer, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "the data mover exited with code 23")
	require.ErrorContains(t, err, `rsync documents this exit code as: Partial transfer due to error`)

	out := writer.String()
	assert.Contains(t, out, fakePodLogs, "the log tail is the most useful thing to show and was being dropped")
	assert.True(t, strings.HasSuffix(strings.TrimRight(out, "\n"), fakePodLogs),
		"nothing may reach the writer after the tail, or the progress bar interleaves with it")
}

// fakePodLogs is what the fake clientset serves for any pod log request.
const fakePodLogs = "fake logs"

// syncWriter serialises the writes the progress bar and the log tail both make.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p) //nolint:wrapcheck
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}

// TestWaitForJobCompletion_FinishedJobWithoutPods: a failed job whose pod was
// deleted (mid-run kill, node drain, TTL) must fail fast with what the job
// still knows, not block on a watch for a pod that will never come.
func TestWaitForJobCompletion_FinishedJobWithoutPods(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-rsync", Namespace: "default"},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{
				{
					Type:    batchv1.JobFailed,
					Status:  corev1.ConditionTrue,
					Message: "Job has reached the specified backoff limit",
				},
			},
		},
	})

	start := time.Now()

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", false, false, console.Palette{},
		&bytes.Buffer{}, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "job default/test-rsync failed and its pod is gone")
	require.ErrorContains(t, err, "backoff limit")
	assert.Less(t, time.Since(start), 30*time.Second, "must not wait out the pod watch timeout")
}

// TestWaitForJobCompletion_PodFailsBeforeRunning covers the pre-follower branch:
// a pod that goes Pending straight to Failed is finished by the terminal-pod
// path, with the same explanation as any other failure.
func TestWaitForJobCompletion_PodFailsBeforeRunning(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rsync"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "rsync",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 11}},
				},
			},
		},
	})

	writer := &bytes.Buffer{}

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", false, false, console.Palette{},
		writer, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, `rsync documents this exit code as: Error in file I/O`)
	assert.Contains(t, writer.String(), fakePodLogs)
}

// TestWaitForJobCompletion_StructuredLogsKeepTheWriterClean: with a structured
// log stream, the raw tail on the writer would corrupt the records around it,
// so it has to travel inside a record instead.
func TestWaitForJobCompletion_StructuredLogsKeepTheWriterClean(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()

	createPod(t, cli, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rsync-abc",
			Namespace: "default",
			Labels:    map[string]string{"job-name": "test-rsync"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "rsync",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 23}},
				},
			},
		},
	})

	var (
		writer  bytes.Buffer
		records bytes.Buffer
	)

	err := k8s.WaitForJobCompletion(t.Context(), cli, "default", "test-rsync", false, true, console.Palette{},
		&writer, slog.New(slog.NewJSONHandler(&records, nil)))
	require.ErrorContains(t, err, "the data mover exited with code 23")

	assert.Empty(t, writer.String(), "raw text on a structured stream corrupts it")
	assert.Contains(t, records.String(), fakePodLogs, "the tail still has to reach the user")
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

			job, err := k8s.FindDataMoverJob(ctx, cli, tt.ns, tt.prefix, slog.New(slog.DiscardHandler))
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

func createPod(t *testing.T, cli kubernetes.Interface, pod *corev1.Pod) {
	t.Helper()

	_, err := cli.CoreV1().Pods(pod.Namespace).Create(t.Context(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
}
