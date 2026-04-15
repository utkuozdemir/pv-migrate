package bucketstorage_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/utkuozdemir/pv-migrate/internal/bucketstorage"
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

	got, err := bucketstorage.MergeHelmValues(baseValues, &bucketstorage.Request{ImageTag: "v1.2.3"},
		slog.New(slog.DiscardHandler))
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

	got, err := bucketstorage.MergeHelmValues(baseValues, req, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, "custom", rcloneImageTag(t, got))
}

func TestShouldUploadMetadata_SkipsDryRun(t *testing.T) {
	t.Parallel()

	for _, extraArgs := range []string{"--dry-run", "--dry-run=true", "-n"} {
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

func TestShouldUploadMetadata_IgnoresDryRunFalse(t *testing.T) {
	t.Parallel()

	req := &bucketstorage.Request{
		Direction:       rclone.DirectionBackup,
		RcloneExtraArgs: "--dry-run=false",
	}

	assert.True(t, bucketstorage.ShouldUploadMetadata(req))
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

func testPVCInfo(name string) *pvc.Info {
	return &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
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
