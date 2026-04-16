package rclone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

func TestBuildCommand_Backup(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		Direction:  rclone.DirectionBackup,
		RemotePath: "remote:my-bucket/default/my-pvc/",
		LocalPath:  "/data",
	}

	result, err := cmd.Build()
	require.NoError(t, err)
	assert.Equal(
		t,
		"rclone sync --stats 1s --stats-log-level NOTICE "+
			"--use-json-log --stats-one-line '/data' 'remote:my-bucket/default/my-pvc/'",
		result,
	)
}

func TestBuildCommand_Restore(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		Direction:  rclone.DirectionRestore,
		RemotePath: "remote:my-bucket/default/my-pvc/",
		LocalPath:  "/data",
	}

	result, err := cmd.Build()
	require.NoError(t, err)
	assert.Equal(
		t,
		"rclone sync --stats 1s --stats-log-level NOTICE "+
			"--use-json-log --stats-one-line 'remote:my-bucket/default/my-pvc/' '/data'",
		result,
	)
}

func TestBuildCommand_WithConfigPath(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		Direction:  rclone.DirectionBackup,
		RemotePath: "remote:bucket/",
		LocalPath:  "/data",
		ConfigPath: "/etc/rclone/rclone.conf",
	}

	result, err := cmd.Build()
	require.NoError(t, err)
	assert.Equal(
		t,
		"rclone sync --config '/etc/rclone/rclone.conf' "+
			"--stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line '/data' 'remote:bucket/'",
		result,
	)
}

func TestBuildCommand_WithExtraArgs(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		Direction:  rclone.DirectionBackup,
		RemotePath: "remote:bucket/path/",
		LocalPath:  "/data",
		ExtraArgs:  "--dry-run --verbose",
	}

	result, err := cmd.Build()
	require.NoError(t, err)
	assert.Equal(
		t,
		"rclone sync --stats 1s --stats-log-level NOTICE --use-json-log "+
			"--stats-one-line '/data' 'remote:bucket/path/' --dry-run --verbose",
		result,
	)
}

func TestBuildCommand_QuotesShellArgs(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		Direction:  rclone.DirectionBackup,
		RemotePath: "remote:bucket/path with 'quote'/",
		LocalPath:  "/data/my dir",
		ConfigPath: "/etc/rclone/rclone.conf",
	}

	result, err := cmd.Build()
	require.NoError(t, err)
	assert.Equal(
		t,
		"rclone sync --config '/etc/rclone/rclone.conf' "+
			"--stats 1s --stats-log-level NOTICE --use-json-log --stats-one-line "+
			"'/data/my dir' 'remote:bucket/path with '\"'\"'quote'\"'\"'/'",
		result,
	)
}

func TestBuildCommand_EmptyDirection_ReturnsError(t *testing.T) {
	t.Parallel()

	cmd := rclone.Cmd{
		RemotePath: "remote:bucket/",
		LocalPath:  "/data",
	}

	_, err := cmd.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "direction")
}

func TestBuildRemotePath(t *testing.T) {
	t.Parallel()

	result := rclone.BuildRemotePath("my-bucket", "pv-migrate", "my-backup")
	assert.Equal(t, "remote:my-bucket/pv-migrate/my-backup/", result)
}

func TestBuildRemotePath_EmptyPrefix(t *testing.T) {
	t.Parallel()

	result := rclone.BuildRemotePath("my-bucket", "", "my-backup")
	assert.Equal(t, "remote:my-bucket/my-backup/", result)
}

func TestBuildMetadataRemotePath(t *testing.T) {
	t.Parallel()

	result := rclone.BuildMetadataRemotePath("my-bucket", "pv-migrate", "my-backup")
	assert.Equal(t, "remote:my-bucket/pv-migrate/my-backup.meta.yaml", result)
}

func TestBuildMetadataRemotePath_EmptyPrefix(t *testing.T) {
	t.Parallel()

	result := rclone.BuildMetadataRemotePath("my-bucket", "", "my-backup")
	assert.Equal(t, "remote:my-bucket/my-backup.meta.yaml", result)
}

func TestBuildRemotePathRaw(t *testing.T) {
	t.Parallel()

	result := rclone.BuildRemotePathRaw("myremote:bucket/path")
	assert.Equal(t, "myremote:bucket/path", result)
}
