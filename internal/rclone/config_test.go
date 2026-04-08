package rclone_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

func TestGenerateConfig_S3_WithCredentials(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:   rclone.BackendS3,
		Endpoint:  "https://minio.example.com",
		Region:    "us-east-1",
		AccessKey: "AKID",
		SecretKey: "SKEY",
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "[remote]")
	assert.Contains(t, conf, "type = s3")
	assert.Contains(t, conf, "provider = Other")
	assert.Contains(t, conf, "endpoint = https://minio.example.com")
	assert.Contains(t, conf, "region = us-east-1")
	assert.Contains(t, conf, "access_key_id = AKID")
	assert.Contains(t, conf, "secret_access_key = SKEY")
	assert.NotContains(t, conf, "env_auth")
}

func TestGenerateConfig_S3_NoCredentials_UsesEnvAuth(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:  rclone.BackendS3,
		Endpoint: "https://s3.amazonaws.com",
		Region:   "eu-west-1",
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "env_auth = true")
	assert.NotContains(t, conf, "access_key_id")
	assert.NotContains(t, conf, "secret_access_key")
}

func TestGenerateConfig_S3_NoEndpoint(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:   rclone.BackendS3,
		Region:    "us-east-1",
		AccessKey: "AKID",
		SecretKey: "SKEY",
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.NotContains(t, conf, "endpoint")
}

func TestGenerateConfig_Azure_WithKey(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:        rclone.BackendAzure,
		StorageAccount: "myaccount",
		StorageKey:     "base64key==",
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "[remote]")
	assert.Contains(t, conf, "type = azureblob")
	assert.Contains(t, conf, "account = myaccount")
	assert.Contains(t, conf, "key = base64key==")
	assert.NotContains(t, conf, "env_auth")
}

func TestGenerateConfig_Azure_NoKey_UsesEnvAuth(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:        rclone.BackendAzure,
		StorageAccount: "myaccount",
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "env_auth = true")
	assert.NotContains(t, conf, "key =")
}

func TestGenerateConfig_GCS_EnvAuth(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend: rclone.BackendGCS,
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "[remote]")
	assert.Contains(t, conf, "type = google cloud storage")
	assert.Contains(t, conf, "env_auth = true")
}

func TestGenerateConfig_GCS_WithServiceAccountCredentials(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend:               rclone.BackendGCS,
		GCSServiceAccountJSON: `{"type":"service_account","project_id":"test"}`,
	}

	conf, err := rclone.GenerateConfig(opts)
	require.NoError(t, err)
	assert.Contains(t, conf, "type = google cloud storage")
	assert.Contains(t, conf, `service_account_credentials = {"type":"service_account","project_id":"test"}`)
	assert.NotContains(t, conf, "env_auth")
}

func TestGenerateConfig_UnknownBackend_ReturnsError(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{
		Backend: "ftp",
	}

	_, err := rclone.GenerateConfig(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported backend")
}

func TestGenerateConfig_EmptyBackend_ReturnsError(t *testing.T) {
	t.Parallel()

	opts := rclone.ConfigOptions{}

	_, err := rclone.GenerateConfig(opts)
	require.Error(t, err)
}

func TestReadConfigFile(t *testing.T) {
	t.Parallel()

	content := "[myremote]\ntype = s3\nprovider = AWS\n"
	path := t.TempDir() + "/rclone.conf"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	result, err := rclone.ReadConfigFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestReadConfigFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := rclone.ReadConfigFile("/nonexistent/rclone.conf")
	require.Error(t, err)
}
