package app_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/app"
)

func TestBackupCmd_RawConfigDoesNotRequireName(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	cmd, err := app.BuildMigrateCmd(context.Background(), "dev", "commit", "date", logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"backup",
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

	cmd, err := app.BuildMigrateCmd(context.Background(), "dev", "commit", "date", logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"restore",
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

	cmd, err := app.BuildMigrateCmd(context.Background(), "dev", "commit", "date", logger)
	require.NoError(t, err)

	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"restore",
		"--rclone-config", "/tmp/missing-rclone.conf",
		"--remote", "manual:bucket/path",
	})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `required flag(s) "dest" not set`)
}

func TestApplyBucketStorageEnvDefaults(t *testing.T) {
	t.Setenv(app.EnvS3AccessKey, "access")
	t.Setenv(app.EnvS3SecretKey, "secret")
	t.Setenv(app.EnvAzureStorageAccount, "account")
	t.Setenv(app.EnvAzureStorageKey, "key")
	t.Setenv(app.EnvGCSServiceAccountJSON, `{"type":"service_account"}`)

	var accessKey, secretKey, storageAccount, storageKey, gcsServiceAccountJSON string

	app.ApplyBucketStorageEnvDefaults(&accessKey, &secretKey, &storageAccount, &storageKey, &gcsServiceAccountJSON)

	assert.Equal(t, "access", accessKey)
	assert.Equal(t, "secret", secretKey)
	assert.Equal(t, "account", storageAccount)
	assert.Equal(t, "key", storageKey)
	assert.JSONEq(t, `{"type":"service_account"}`, gcsServiceAccountJSON)
}

func TestApplyBucketStorageEnvDefaults_DoesNotOverrideExplicitValues(t *testing.T) {
	t.Setenv(app.EnvS3AccessKey, "env-access")
	t.Setenv(app.EnvS3SecretKey, "env-secret")

	accessKey := "flag-access"
	secretKey := "flag-secret"

	var storageAccount, storageKey, gcsServiceAccountJSON string

	app.ApplyBucketStorageEnvDefaults(&accessKey, &secretKey, &storageAccount, &storageKey, &gcsServiceAccountJSON)

	assert.Equal(t, "flag-access", accessKey)
	assert.Equal(t, "flag-secret", secretKey)
}
