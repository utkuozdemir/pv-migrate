package app

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupCmd_RawConfigDoesNotRequireName(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	cmd, err := buildBackupCmd(&logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--source", "test-pvc",
		"--source-kubeconfig", "/tmp/missing-kubeconfig",
		"--rclone-config", "/tmp/missing-rclone.conf",
		"--remote", "manual:bucket/path",
	})

	err = cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), `required flag(s) "name" not set`)
	assert.Contains(t, err.Error(), "failed to read rclone config file")
}

func TestRestoreCmd_UsesDestinationFlags(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	cmd, err := buildRestoreCmd(&logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--dest", "test-pvc",
		"--dest-kubeconfig", "/tmp/missing-kubeconfig",
		"--rclone-config", "/tmp/missing-rclone.conf",
		"--remote", "manual:bucket/path",
	})

	err = cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), `required flag(s) "dest" not set`)
	assert.Contains(t, err.Error(), "failed to read rclone config file")
}

func TestRestoreCmd_RequiresDestination(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	cmd, err := buildRestoreCmd(&logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--rclone-config", "/tmp/missing-rclone.conf",
		"--remote", "manual:bucket/path",
	})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `required flag(s) "dest" not set`)
}

func TestApplyBucketStorageEnvDefaults(t *testing.T) {
	t.Setenv(envS3AccessKey, "access")
	t.Setenv(envS3SecretKey, "secret")
	t.Setenv(envAzureStorageAccount, "account")
	t.Setenv(envAzureStorageKey, "key")
	t.Setenv(envGCSServiceAccountJSON, `{"type":"service_account"}`)

	var accessKey, secretKey, storageAccount, storageKey, gcsServiceAccountJSON string

	applyBucketStorageEnvDefaults(&accessKey, &secretKey, &storageAccount, &storageKey, &gcsServiceAccountJSON)

	assert.Equal(t, "access", accessKey)
	assert.Equal(t, "secret", secretKey)
	assert.Equal(t, "account", storageAccount)
	assert.Equal(t, "key", storageKey)
	assert.JSONEq(t, `{"type":"service_account"}`, gcsServiceAccountJSON)
}

func TestApplyBucketStorageEnvDefaults_DoesNotOverrideExplicitValues(t *testing.T) {
	t.Setenv(envS3AccessKey, "env-access")
	t.Setenv(envS3SecretKey, "env-secret")

	accessKey := "flag-access"
	secretKey := "flag-secret"

	var storageAccount, storageKey, gcsServiceAccountJSON string

	applyBucketStorageEnvDefaults(&accessKey, &secretKey, &storageAccount, &storageKey, &gcsServiceAccountJSON)

	assert.Equal(t, "flag-access", accessKey)
	assert.Equal(t, "flag-secret", secretKey)
}
