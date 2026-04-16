//go:build integration && integration_cloud

package integration

import (
	"os"
	"testing"

	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

const (
	defaultS3Bucket       = "pv-migrate-test"
	defaultS3Region       = "us-east-1"
	defaultGCSBucket      = "pv-migrate-test"
	defaultAzureContainer = "pv-migrate-test"
	defaultAzureAccount   = "pvmigratetest"
)

func TestCloudS3(t *testing.T) {
	t.Parallel()

	accessKey := os.Getenv("CLOUD_TEST_S3_ACCESS_KEY")
	secretKey := os.Getenv("CLOUD_TEST_S3_SECRET_KEY")

	if accessKey == "" || secretKey == "" {
		skipOrFail(t, "CLOUD_TEST_S3_ACCESS_KEY / CLOUD_TEST_S3_SECRET_KEY not set")
	}

	infra := setupBackupInfra(t)
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	cloudRoundTrip(t, infra, ns, ns+"-cloud-s3", pvmigrate.Backup{
		Backend:   "s3",
		Bucket:    envOrDefault("CLOUD_TEST_S3_BUCKET", defaultS3Bucket),
		Region:    envOrDefault("CLOUD_TEST_S3_REGION", defaultS3Region),
		AccessKey: accessKey,
		SecretKey: secretKey,
	})
}

func TestCloudGCS(t *testing.T) {
	t.Parallel()

	saJSON := os.Getenv("CLOUD_TEST_GCS_SERVICE_ACCOUNT_JSON")

	if saJSON == "" {
		skipOrFail(t, "CLOUD_TEST_GCS_SERVICE_ACCOUNT_JSON not set")
	}

	infra := setupBackupInfra(t)
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	cloudRoundTrip(t, infra, ns, ns+"-cloud-gcs", pvmigrate.Backup{
		Backend:               "gcs",
		Bucket:                envOrDefault("CLOUD_TEST_GCS_BUCKET", defaultGCSBucket),
		GCSServiceAccountJSON: saJSON,
	})
}

func TestCloudAzure(t *testing.T) {
	t.Parallel()

	storageKey := os.Getenv("CLOUD_TEST_AZURE_STORAGE_KEY")

	if storageKey == "" {
		skipOrFail(t, "CLOUD_TEST_AZURE_STORAGE_KEY not set")
	}

	infra := setupBackupInfra(t)
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	cloudRoundTrip(t, infra, ns, ns+"-cloud-azure", pvmigrate.Backup{
		Backend:        "azure",
		Bucket:         envOrDefault("CLOUD_TEST_AZURE_CONTAINER", defaultAzureContainer),
		StorageAccount: envOrDefault("CLOUD_TEST_AZURE_STORAGE_ACCOUNT", defaultAzureAccount),
		StorageKey:     storageKey,
	})
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return fallback
}

// skipOrFail skips the test locally but fails in CI when cloud tests are required.
func skipOrFail(t *testing.T, msg string) {
	t.Helper()

	if os.Getenv("CLOUD_TESTS_MUST_RUN") != "" {
		t.Fatalf("%s (CLOUD_TESTS_MUST_RUN is set)", msg)
	}

	t.Skip(msg)
}

// cloudRoundTrip performs a backup-wipe-restore-verify cycle.
// The template provides backend-specific fields; common fields are filled in automatically.
func cloudRoundTrip(t *testing.T, infra *backupTestInfra, ns, backupName string, template pvmigrate.Backup) {
	t.Helper()

	template.PVC = pvmigrate.PVC{Namespace: ns, Name: "test-pvc"}
	template.Name = backupName
	template.IgnoreMounted = true
	template.NoCleanupOnFailure = true
	template.HelmValues = infra.rcloneHelmVals
	template.Logger = slogt.New(t)
	template.Writer = os.Stderr

	err := pvmigrate.RunBackup(t.Context(), template)
	require.NoError(t, err)

	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "rm -f "+backupTestDataPath)
	require.NoError(t, err)

	restore := pvmigrate.Restore{
		PVC:                   template.PVC,
		Backend:               template.Backend,
		Bucket:                template.Bucket,
		Region:                template.Region,
		AccessKey:             template.AccessKey,
		SecretKey:             template.SecretKey,
		StorageAccount:        template.StorageAccount,
		StorageKey:            template.StorageKey,
		GCSServiceAccountJSON: template.GCSServiceAccountJSON,
		Name:                  backupName,
		IgnoreMounted:         true,
		NoCleanupOnFailure:    true,
		HelmValues:            infra.rcloneHelmVals,
		Logger:                slogt.New(t),
		Writer:                os.Stderr,
	}

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	output, err := execInPod(t.Context(), infra.cli, ns, "test-pod", "cat "+backupTestDataPath)
	require.NoError(t, err)
	assert.Equal(t, backupTestDataContent, output)
}
