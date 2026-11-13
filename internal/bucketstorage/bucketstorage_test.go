package bucketstorage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/utkuozdemir/pv-migrate/internal/bucketstorage"
	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

func TestManagedPathValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		prefix  bool
		wantErr string
	}{
		{
			name:  "valid name",
			value: "backup-2026.04.24",
		},
		{
			name:   "allows slash separated prefix",
			value:  "team-a/backups",
			prefix: true,
		},
		{
			name:    "rejects invalid name",
			value:   "bad/name",
			wantErr: `--name "bad/name" contains invalid characters`,
		},
		{
			name:    "rejects empty prefix segment",
			value:   "team-a//backups",
			prefix:  true,
			wantErr: `must not have leading/trailing '/' or empty path segments`,
		},
		{
			name:    "rejects invalid prefix segment characters",
			value:   "team-a/bad prefix",
			prefix:  true,
			wantErr: `--prefix "team-a/bad prefix" contains invalid characters`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tt.prefix {
				err = bucketstorage.ValidatePrefix(tt.value)
			} else {
				err = bucketstorage.ValidateName(tt.value)
			}

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

//nolint:funlen
func TestBuildRemotePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      bucketstorage.Request
		expected string
		wantErr  string
	}{
		{
			name: "raw config mode bypasses name and bucket requirements",
			req: bucketstorage.Request{
				RcloneConfigFile: "/tmp/rclone.conf",
				Remote:           "manual:bucket/path",
			},
			expected: "manual:bucket/path",
		},
		{
			name: "raw config mode requires remote",
			req: bucketstorage.Request{
				RcloneConfigFile: "/tmp/rclone.conf",
			},
			wantErr: "--remote is required when using --rclone-config",
		},
		{
			name: "standard mode requires bucket",
			req: bucketstorage.Request{
				Name: "backup",
			},
			wantErr: "--bucket is required",
		},
		{
			name: "standard mode requires name",
			req: bucketstorage.Request{
				Bucket: "bucket",
			},
			wantErr: "--name is required",
		},
		{
			name: "allows slash separated prefix",
			req: bucketstorage.Request{
				Bucket: "bucket",
				Prefix: "team-a/backups",
				Name:   "backup",
			},
			expected: "remote:bucket/team-a/backups/backup/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := bucketstorage.BuildRemotePath(&tt.req)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeHelmValues_ImageTagInjected(t *testing.T) {
	t.Parallel()

	baseValues := map[string]any{
		"rclone": map[string]any{
			"image": map[string]any{
				"repository": "docker.io/utkuozdemir/pv-migrate-rclone",
				"tag":        "latest",
			},
		},
	}

	got, err := bucketstorage.MergeHelmValues(baseValues, &bucketstorage.Request{ImageTag: "v1.2.3"})
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", rcloneImageTag(t, got))
}

func TestMergeHelmValues_HelmSetOverridesImageTag(t *testing.T) {
	t.Parallel()

	baseValues := map[string]any{
		"rclone": map[string]any{
			"image": map[string]any{
				"repository": "docker.io/utkuozdemir/pv-migrate-rclone",
				"tag":        "latest",
			},
		},
	}

	req := &bucketstorage.Request{
		ImageTag:   "v1.2.3",
		HelmValues: []string{"rclone.image.tag=custom"},
	}

	got, err := bucketstorage.MergeHelmValues(baseValues, req)
	require.NoError(t, err)
	assert.Equal(t, "custom", rcloneImageTag(t, got))
}

func TestShouldUploadMetadata_SkipsDryRun(t *testing.T) {
	t.Parallel()

	// rclone parses its flags with pflag, so a short flag can be bundled with
	// others. A bundled -n used to go unnoticed, and the backup then wrote a
	// metadata object for a run that transferred nothing.
	for _, extraArgs := range []string{
		"--dry-run", "-n", "-nv", "-vn", "-v -n", "-Pnv",
		// pflag reads a boolean flag's value with strconv.ParseBool
		"--dry-run=true", "--dry-run=TRUE", "--dry-run=True", "--dry-run=1", "--dry-run=t",
		"-n=true", "-n=1",
	} {
		t.Run(extraArgs, func(t *testing.T) {
			t.Parallel()

			req := &bucketstorage.Request{
				Direction:       rclone.DirectionBackup,
				RcloneExtraArgs: extraArgs,
			}

			assert.False(t, bucketstorage.ShouldUploadMetadata(req))
		})
	}
}

// TestShouldUploadMetadata_NotFooledByValues covers the other direction. This is
// a heuristic over a raw shell fragment, not a parse of it, so it has to err
// towards uploading: reading a dry run into an ordinary backup would leave that
// backup without the metadata that identifies it.
func TestShouldUploadMetadata_NotFooledByValues(t *testing.T) {
	t.Parallel()

	for _, extraArgs := range []string{
		"--dry-run=false", "--dry-run=FALSE", "--dry-run=0", "--dry-run=f",
		"-n=false", "-n=0",
		"--dry-run=notabool",
		"--transfers -1n",
		"--exclude nope",
		"--bwlimit 1n",
		"",
	} {
		t.Run(extraArgs, func(t *testing.T) {
			t.Parallel()

			req := &bucketstorage.Request{
				Direction:       rclone.DirectionBackup,
				RcloneExtraArgs: extraArgs,
			}

			assert.True(t, bucketstorage.ShouldUploadMetadata(req))
		})
	}
}

func TestBuildHelmValues_MetadataPresentWhenGenerated(t *testing.T) {
	t.Parallel()

	info := testPVCInfo("src")
	req := &bucketstorage.Request{
		Direction: rclone.DirectionBackup,
	}

	got := bucketstorage.BuildHelmValues("default", req, info, "conf", "cmd", true, "metadata", "remote:path.meta.yaml")
	rcloneVals := got["rclone"].(map[string]any) //nolint:forcetypeassert

	assert.Equal(t, "metadata", rcloneVals["metadataBase64"])
	assert.Equal(t, "remote:path.meta.yaml", rcloneVals["metadataRemotePath"])
}

func TestRunRelease_CleansUpFailedInstall(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op1-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	releases, initCfg := fakeHelmConfig(&kubefake.FailingKubeClient{
		Out:       io.Discard,
		WaitError: errors.New("timed out waiting for the condition"),
	})

	stuckPod := testJobPod(releaseName, corev1.PodPending)

	var out, logs bytes.Buffer

	info := testClusterPVCInfo(stuckPod)
	req := &bucketstorage.Request{
		Direction:   rclone.DirectionBackup,
		HelmTimeout: time.Minute,
		Writer:      &out,
	}

	err = bucketstorage.RunRelease(t.Context(), helmChart, info, releaseName, "op1",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorContains(t, err, "failed to install helm chart")
	require.ErrorContains(t, err, "timed out waiting for the condition")

	_, histErr := releases.History(releaseName)
	require.ErrorIs(t, histErr, driver.ErrReleaseNotFound)
	assert.Contains(t, logs.String(), "Removed release")
	assert.Contains(t, out.String(), "What the cluster reported:")
	assert.Contains(t, out.String(), stuckPod.Name)
}

func TestRunRelease_HonorsNoCleanupOnFailureForInstall(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op2-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	releases, initCfg := fakeHelmConfig(&kubefake.FailingKubeClient{
		Out:       io.Discard,
		WaitError: errors.New("timed out waiting for the condition"),
	})

	var out, logs bytes.Buffer

	info := testClusterPVCInfo()
	req := &bucketstorage.Request{
		Direction:          rclone.DirectionBackup,
		NoCleanupOnFailure: true,
		HelmTimeout:        time.Minute,
		Writer:             &out,
	}

	err = bucketstorage.RunRelease(t.Context(), helmChart, info, releaseName, "op2",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorContains(t, err, "failed to install helm chart")

	history, histErr := releases.History(releaseName)
	require.NoError(t, histErr)
	assert.Len(t, history, 1)
	assert.Contains(t, logs.String(), "Skipped cleanup because the operation failed")
}

func TestRunRelease_IgnoresMissingReleaseOnEarlyInstallFailure(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op3-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	_, initCfg := fakeHelmConfig(&kubefake.FailingKubeClient{
		Out:        io.Discard,
		BuildError: errors.New("manifest build failed"),
	})

	var logs bytes.Buffer

	info := testClusterPVCInfo()
	req := &bucketstorage.Request{
		Direction:   rclone.DirectionBackup,
		HelmTimeout: time.Minute,
		Writer:      io.Discard,
	}

	err = bucketstorage.RunRelease(t.Context(), helmChart, info, releaseName, "op3",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorContains(t, err, "failed to install helm chart")
	assert.NotContains(t, logs.String(), "Cleanup failed")
	assert.NotContains(t, logs.String(), "Removed release")
}

func TestRunRelease_PreservesExistingReleaseOnNameConflict(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op6-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	releases, initCfg := fakeHelmConfig(&kubefake.PrintingKubeClient{Out: io.Discard})
	require.NoError(t, releases.Create(&releasev1.Release{
		Name:      releaseName,
		Namespace: "default",
		Version:   1,
		Info: &releasev1.Info{
			Status:      releasecommon.StatusDeployed,
			Description: "Install complete",
		},
	}))

	var out, logs bytes.Buffer

	info := testClusterPVCInfo(testJobPod(releaseName, corev1.PodRunning))
	req := &bucketstorage.Request{
		Direction:   rclone.DirectionBackup,
		HelmTimeout: time.Minute,
		Writer:      &out,
	}

	err = bucketstorage.RunRelease(t.Context(), helmChart, info, releaseName, "op6",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorContains(t, err, "cannot reuse a name that is still in use")

	history, histErr := releases.History(releaseName)
	require.NoError(t, histErr)
	require.Len(t, history, 1)
	assert.Empty(t, out.String())
	assert.NotContains(t, logs.String(), "Removed release")
}

func TestRunRelease_CleansUpWhenDetachWaitFails(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op4-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	releases, initCfg := fakeHelmConfig(&kubefake.PrintingKubeClient{Out: io.Discard})
	stuckPod := testJobPod(releaseName, corev1.PodPending)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cli := fake.NewClientset(stuckPod)
	cli.PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) {
		cancel()

		return true, watch.NewEmptyWatch(), nil
	})

	info := &pvc.Info{
		Claim:         &corev1.PersistentVolumeClaim{Namespace: "default", Name: "src"},
		ClusterClient: &k8s.ClusterClient{KubeClient: cli},
	}

	var out, logs bytes.Buffer

	req := &bucketstorage.Request{
		Direction:   rclone.DirectionBackup,
		Detach:      true,
		HelmTimeout: time.Minute,
		Writer:      &out,
	}

	err = bucketstorage.RunRelease(ctx, helmChart, info, releaseName, "op4",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.ErrorContains(t, err, "failed to wait for job to start")

	_, histErr := releases.History(releaseName)
	require.ErrorIs(t, histErr, driver.ErrReleaseNotFound)
	assert.Contains(t, logs.String(), "Removed release")
	assert.Contains(t, out.String(), "What the cluster reported:")
	assert.Contains(t, out.String(), stuckPod.Name)
	assert.NotContains(t, out.String(), "To check status:")
}

//nolint:paralleltest // the feature gate below is process-wide
func TestRunRelease_KeepsReleaseWhenDetachSucceeds(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	const releaseName = "pv-migrate-op5-backup"

	helmChart, err := helm.LoadChart("0.0.0-test")
	require.NoError(t, err)

	releases, initCfg := fakeHelmConfig(&kubefake.PrintingKubeClient{Out: io.Discard})
	runningPod := testJobPod(releaseName, corev1.PodRunning)
	runningPod.Spec.NodeName = "node-1"

	var out, logs bytes.Buffer

	info := testClusterPVCInfo(runningPod)
	req := &bucketstorage.Request{
		Direction:   rclone.DirectionBackup,
		Detach:      true,
		HelmTimeout: time.Minute,
		Writer:      &out,
	}

	err = bucketstorage.RunRelease(t.Context(), helmChart, info, releaseName, "op5",
		testBackupValues(req, info), req, initCfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)

	history, histErr := releases.History(releaseName)
	require.NoError(t, histErr)
	assert.Len(t, history, 1)
	assert.Contains(t, out.String(), "pv-migrate status op5")
	assert.NotContains(t, logs.String(), "Removed release")
	assert.NotContains(t, logs.String(), "Backup succeeded")
}

func fakeHelmConfig(kubeClient kube.Interface) (*storage.Storage, bucketstorage.HelmActionConfigFunc) {
	releases := storage.Init(driver.NewMemory())
	cfg := &action.Configuration{
		Releases:     releases,
		KubeClient:   kubeClient,
		Capabilities: common.DefaultCapabilities,
	}

	return releases, func(*pvc.Info) (*action.Configuration, error) {
		return cfg, nil
	}
}

func testBackupValues(req *bucketstorage.Request, info *pvc.Info) map[string]any {
	return bucketstorage.BuildHelmValues(
		info.Claim.Namespace,
		req,
		info,
		"[remote]\ntype = s3\n",
		"rclone sync /data remote:b/n/",
		true,
		"",
		"",
	)
}

func testJobPod(releaseName string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		Namespace: "default",
		Name:      releaseName + "-rclone-pod",
		Labels: map[string]string{
			"app.kubernetes.io/instance": releaseName,
			"job-name":                   releaseName + "-rclone",
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

func testClusterPVCInfo(objects ...runtime.Object) *pvc.Info {
	return &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{
			Namespace: "default",
			Name:      "src",
		},
		ClusterClient: &k8s.ClusterClient{
			KubeClient: fake.NewClientset(objects...),
		},
	}
}

func testPVCInfo(name string) *pvc.Info {
	return &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{
			Name: name,
		},
	}
}

func rcloneImageTag(t *testing.T, vals map[string]any) string {
	t.Helper()

	rcloneVals, ok := vals["rclone"].(map[string]any)
	require.True(t, ok)

	imageVals, ok := rcloneVals["image"].(map[string]any)
	require.True(t, ok)

	tag, ok := imageVals["tag"].(string)
	require.True(t, ok)

	return tag
}
