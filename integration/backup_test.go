//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

const (
	s3Endpoint  = "http://seaweedfs.pv-migrate-test.svc:8333"
	s3Bucket    = "pv-migrate-test"
	s3AccessKey = "pv-migrate"
	s3SecretKey = "pv-migrate"

	// verifyImage reads the bucket back. It is upstream rclone rather than the
	// image under test, so the check does not share that image's mistakes.
	// renovate: depName=rclone/rclone datasource=docker
	verifyImage = "docker.io/rclone/rclone:1.76.0"

	backupTestDataContent = "BACKUP_DATA"
	backupTestDataPath    = "/volume/backup_test.txt"
)

// backupTestInfra holds shared infrastructure for backup/restore tests.
type backupTestInfra struct {
	cli            *k8s.ClusterClient
	rcloneHelmVals []string
}

func setupBackupInfra(t *testing.T) *backupTestInfra {
	t.Helper()

	logger := slogt.New(t)
	slog.SetDefault(logger)
	klog.SetSlogLogger(logger)

	cli, err := k8s.GetClusterClient("", "", logger)
	require.NoError(t, err)

	infra := &backupTestInfra{cli: cli}

	if img := os.Getenv("RCLONE_IMAGE"); img != "" {
		parts := strings.SplitN(img, ":", 2)
		require.Len(t, parts, 2, "RCLONE_IMAGE must be in repository:tag format")

		infra.rcloneHelmVals = []string{
			"rclone.image.repository=" + parts[0],
			"rclone.image.tag=" + parts[1],
		}
	}

	return infra
}

// seedAndCreatePVC creates a namespace, PVC, and pod with test data.
// Returns the namespace name.
func seedAndCreatePVC(t *testing.T, infra *backupTestInfra, seedCmd string) string {
	t.Helper()

	ns := newTestNS(t, infra.cli, "pvmig-bak")

	err := provisionPod(t.Context(), infra.cli, ns, "test-pvc", "test-pod", seedCmd)
	require.NoError(t, err)

	return ns
}

func backupSeedCmd() string {
	return fmt.Sprintf("echo -n %s > %s", backupTestDataContent, backupTestDataPath)
}

func TestBackupRestore(t *testing.T) {
	t.Parallel()

	infra := setupBackupInfra(t)

	t.Run("BackupBasic", func(t *testing.T) {
		t.Parallel()
		testBackupBasic(t, infra)
	})
	t.Run("RestoreBasic", func(t *testing.T) {
		t.Parallel()
		testRestoreBasic(t, infra)
	})
	t.Run("BackupRestoreRoundTrip", func(t *testing.T) {
		t.Parallel()
		testBackupRestoreRoundTrip(t, infra)
	})
	t.Run("BackupCustomPrefix", func(t *testing.T) {
		t.Parallel()
		testBackupCustomPrefix(t, infra)
	})
	t.Run("BackupRcloneExtraArgs", func(t *testing.T) {
		t.Parallel()
		testBackupRcloneExtraArgs(t, infra)
	})
	t.Run("BackupDetach", func(t *testing.T) {
		t.Parallel()
		testBackupDetach(t, infra)
	})
	t.Run("BackupRestoreSubpath", func(t *testing.T) {
		t.Parallel()
		testBackupRestoreSubpath(t, infra)
	})
	t.Run("RestoreDeleteExtraneousFiles", func(t *testing.T) {
		t.Parallel()
		testRestoreDeleteExtraneousFiles(t, infra)
	})
	t.Run("RestorePosixMetadata", func(t *testing.T) {
		t.Parallel()
		testRestorePosixMetadata(t, infra)
	})
	t.Run("RestorePosixMetadataNonRoot", func(t *testing.T) {
		t.Parallel()
		testRestorePosixMetadataNonRoot(t, infra)
	})
	t.Run("RestorePosixMetadataRawRemote", func(t *testing.T) {
		t.Parallel()
		testRestorePosixMetadataRawRemote(t, infra)
	})
	t.Run("RestoreMissingPosixManifestWarns", func(t *testing.T) {
		t.Parallel()
		testRestoreMissingPosixManifestWarns(t, infra)
	})
}

//nolint:thelper // subtest implementation, not a helper
func testBackupBasic(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-basic"
	bucketPath := "pv-migrate/" + backupName + "/"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	// Verify backup data at default prefix
	verifyBucketContents(t, infra, ns, bucketPath)

	// Verify metadata sidecar file
	metaOutput := readBucketFile(t, infra, ns, "pv-migrate/"+backupName+".meta.yaml")
	assert.Contains(t, metaOutput, "version: 1")
	assert.Contains(t, metaOutput, "backupTime:")
	assert.Contains(t, metaOutput, "sourceNamespace: "+ns)
	assert.Contains(t, metaOutput, "sourcePvc: test-pvc")

	// Sync behavior: added files appear after re-backup
	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "echo -n SECOND > /volume/second.txt")
	require.NoError(t, err)

	err = pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	lsOutput := listBucketPath(t, infra, ns, bucketPath)
	assert.Contains(t, lsOutput, "backup_test.txt")
	assert.Contains(t, lsOutput, "second.txt")
	assert.Contains(t, lsOutput, ".pv-migrate-posix.gz")

	// Sync behavior: deleted files are removed from bucket after re-backup
	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "rm /volume/second.txt")
	require.NoError(t, err)

	err = pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	lsOutput = listBucketPath(t, infra, ns, bucketPath)
	assert.Contains(t, lsOutput, "backup_test.txt")
	assert.NotContains(t, lsOutput, "second.txt")
	assert.Contains(t, lsOutput, ".pv-migrate-posix.gz")
}

//nolint:thelper // subtest implementation, not a helper
func testRestoreBasic(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-restore-basic"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "rm -f "+backupTestDataPath)
	require.NoError(t, err)

	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "cat "+backupTestDataPath)
	require.Error(t, err)

	restore := pvmigrate.Restore{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	output, err := execInPod(t.Context(), infra.cli, ns, "test-pod", "cat "+backupTestDataPath)
	require.NoError(t, err)
	assert.Equal(t, backupTestDataContent, output)
}

//nolint:thelper // subtest implementation, not a helper
func testBackupRestoreRoundTrip(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-round-trip"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	err = provisionPod(t.Context(), infra.cli, ns, "dest-pvc", "dest-pod", "")
	require.NoError(t, err)

	restore := pvmigrate.Restore{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "dest-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	output, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "cat "+backupTestDataPath)
	require.NoError(t, err)
	assert.Equal(t, backupTestDataContent, output)
}

//nolint:thelper // subtest implementation, not a helper
func testBackupCustomPrefix(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-custom-prefix"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		Prefix:             "custom-prefix",
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	verifyBucketContents(t, infra, ns, "custom-prefix/"+backupName+"/")
}

//nolint:thelper // subtest implementation, not a helper
func testBackupRcloneExtraArgs(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-extra-args"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		RcloneExtraArgs:    "--dry-run",
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	// With --dry-run, nothing should have been transferred.
	verifyBucketEmpty(t, infra, ns, "pv-migrate/"+backupName+"/")
}

//nolint:thelper // subtest implementation, not a helper
func testBackupDetach(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())

	backupName := ns + "-detach"

	backup := pvmigrate.Backup{
		PVC: pvmigrate.PVC{
			Namespace: ns,
			Name:      "test-pvc",
		},
		ID:                 "detach-test",
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		Detach:             true,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	waitForJobCompletion(t, infra, ns)

	verifyBucketContents(t, infra, ns, "pv-migrate/"+backupName+"/")

	cleanupDetachedRelease(t, infra, ns, "detach-test")
}

//nolint:thelper // subtest implementation, not a helper
func testBackupRestoreSubpath(t *testing.T, infra *backupTestInfra) {
	// Seed PVC with files in a subdirectory and a file at the root.
	seedCmd := "mkdir -p /volume/subdir && " +
		"echo -n SUBDIR_DATA > /volume/subdir/sub.txt && " +
		fmt.Sprintf("echo -n %s > %s", backupTestDataContent, backupTestDataPath)
	ns := seedAndCreatePVC(t, infra, seedCmd)

	backupName := ns + "-subpath"
	bucketPath := "pv-migrate/" + backupName + "/"

	backup := pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		Path:               "subdir",
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	// Only the subdir file should be in the bucket, not the root-level file.
	lsOutput := listBucketPath(t, infra, ns, bucketPath)
	assert.Contains(t, lsOutput, "sub.txt")
	assert.NotContains(t, lsOutput, "backup_test.txt")

	// Wipe the subdir on the PVC, then restore with --path.
	_, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "rm /volume/subdir/sub.txt")
	require.NoError(t, err)

	restore := pvmigrate.Restore{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		Path:               "subdir",
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	// Subdir file should be restored.
	output, err := execInPod(t.Context(), infra.cli, ns, "test-pod", "cat /volume/subdir/sub.txt")
	require.NoError(t, err)
	assert.Equal(t, "SUBDIR_DATA", output)

	// Root-level file should still be intact (restore only touched subdir).
	output, err = execInPod(t.Context(), infra.cli, ns, "test-pod", "cat "+backupTestDataPath)
	require.NoError(t, err)
	assert.Equal(t, backupTestDataContent, output)
}

//nolint:thelper // subtest implementation, not a helper
func testRestoreDeleteExtraneousFiles(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())
	backupName := ns + "-delete-extraneous"

	backup := pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err := pvmigrate.RunBackup(t.Context(), backup)
	require.NoError(t, err)

	err = provisionPod(t.Context(), infra.cli, ns, "dest-pvc", "dest-pod", "echo -n EXTRA > /volume/extra.txt")
	require.NoError(t, err)

	restore := pvmigrate.Restore{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "dest-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	output, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "cat /volume/extra.txt")
	require.NoError(t, err)
	assert.Equal(t, "EXTRA", output)

	restore.DeleteExtraneousFiles = true

	err = pvmigrate.RunRestore(t.Context(), restore)
	require.NoError(t, err)

	_, err = execInPod(t.Context(), infra.cli, ns, "dest-pod", "cat /volume/extra.txt")
	require.Error(t, err)
}

//nolint:thelper // subtest implementation, not a helper
func testRestorePosixMetadata(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, posixMetadataSeedCmd())
	backupName := ns + "-posix-meta"

	srcMtime, err := execInPod(t.Context(), infra.cli, ns, "test-pod", "stat -c '%Y' /volume/secret.txt")
	require.NoError(t, err)

	backup := pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}
	require.NoError(t, pvmigrate.RunBackup(t.Context(), backup))

	require.NoError(t, provisionPod(t.Context(), infra.cli, ns, "dest-pvc", "dest-pod", ""))

	restore := pvmigrate.Restore{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "dest-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}
	require.NoError(t, pvmigrate.RunRestore(t.Context(), restore))

	assertPosixMetadataRestored(t, infra, ns, strings.TrimSpace(srcMtime))

	// Re-restore with --no-chown into another PVC and confirm ownership stays 0:0 while modes survive.
	require.NoError(t, provisionPod(t.Context(), infra.cli, ns, "nochown-pvc", "nochown-pod", ""))

	restore.PVC.Name = "nochown-pvc"
	restore.NoChown = true
	require.NoError(t, pvmigrate.RunRestore(t.Context(), restore))

	assertPodStat(t, infra, ns, "nochown-pod", "/volume/secret.txt", "600 0 0")
	assertPodStat(t, infra, ns, "nochown-pod", "/volume/ro-parent", "500 0 0")
	assertPodStat(t, infra, ns, "nochown-pod", "/volume/setgid-dir", "2775 0 0")
}

func posixMetadataSeedCmd() string {
	return "mkdir -p /volume/empty-sticky /volume/setgid-dir /volume/ro-parent && " +
		"echo -n SECRET > /volume/secret.txt && chmod 0600 /volume/secret.txt && chown 1234:5678 /volume/secret.txt && " +
		"echo -n RUN > /volume/run.sh && chmod 0750 /volume/run.sh && chown 1234:5678 /volume/run.sh && " +
		"echo -n RO > /volume/ro-parent/ro.txt && chmod 0400 /volume/ro-parent/ro.txt && " +
		"chown 1234:5678 /volume/ro-parent/ro.txt && " +
		"chmod 0500 /volume/ro-parent && chown 1234:5678 /volume/ro-parent && " +
		"chmod 2775 /volume/setgid-dir && chown 1234:5678 /volume/setgid-dir && " +
		"chmod 1777 /volume/empty-sticky && chown 1234:5678 /volume/empty-sticky && " +
		"ln -s secret.txt /volume/link-valid && chown -h 1234:5678 /volume/link-valid && " +
		"ln -s missing-target /volume/link-dangling && " +
		"touch -t 202406011230.45 /volume/secret.txt /volume/empty-sticky"
}

func assertPosixMetadataRestored(t *testing.T, infra *backupTestInfra, ns, srcMtime string) {
	t.Helper()

	assertPodStat(t, infra, ns, "dest-pod", "/volume/secret.txt", "600 1234 5678 "+srcMtime)
	assertPodStat(t, infra, ns, "dest-pod", "/volume/run.sh", "750 1234 5678")
	assertPodStat(t, infra, ns, "dest-pod", "/volume/ro-parent", "500 1234 5678")
	assertPodStat(t, infra, ns, "dest-pod", "/volume/ro-parent/ro.txt", "400 1234 5678")
	assertPodStat(t, infra, ns, "dest-pod", "/volume/setgid-dir", "2775 1234 5678")
	assertPodStat(t, infra, ns, "dest-pod", "/volume/empty-sticky", "1777 1234 5678 "+srcMtime)

	linkTarget, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "readlink /volume/link-valid")
	require.NoError(t, err)
	assert.Equal(t, "secret.txt", strings.TrimSpace(linkTarget))

	danglingTarget, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "readlink /volume/link-dangling")
	require.NoError(t, err)
	assert.Equal(t, "missing-target", strings.TrimSpace(danglingTarget))
}

//nolint:thelper // subtest implementation, not a helper
func testRestorePosixMetadataNonRoot(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra,
		"mkdir -p /volume/lost+found /volume/empty-dir /volume/shared && "+
			"chmod 0700 /volume/lost+found && chmod 0750 /volume/empty-dir && "+
			"chown 10000:10000 /volume/empty-dir && chmod 0775 /volume/shared && chown 0:10000 /volume/shared && "+
			"echo -n SHARED > /volume/shared/item.txt && chmod 0660 /volume/shared/item.txt && "+
			"chown 10000:10000 /volume/shared/item.txt && echo -n NR_SECRET > /volume/secret.txt && "+
			"chmod 0600 /volume/secret.txt && chown 10000:10000 /volume/secret.txt && "+
			"touch -t 202406011230.45 /volume/secret.txt")
	backupName := ns + "-posix-nonroot"
	srcMtime, err := execInPod(t.Context(), infra.cli, ns, "test-pod", "stat -c '%Y' /volume/secret.txt")
	require.NoError(t, err)
	enforceRestricted(t, infra.cli, ns)
	require.NoError(t, pvmigrate.RunBackup(t.Context(), pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NonRoot:            true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}))
	destNS := newTestNS(t, infra.cli, "pvmig-bak-nr")
	require.NoError(t, provisionPod(t.Context(), infra.cli, destNS, "dest-pvc", "dest-pod",
		"mkdir -p /volume/lost+found /volume/shared && chmod 0700 /volume/lost+found && "+
			"chmod 0775 /volume/shared && chown 0:10000 /volume/shared && "+
			"echo -n STALE > /volume/stale.txt && chown 10000:10000 /volume/stale.txt"))
	enforceRestricted(t, infra.cli, destNS)
	require.NoError(t, pvmigrate.RunRestore(t.Context(), pvmigrate.Restore{
		PVC:                   pvmigrate.PVC{Namespace: destNS, Name: "dest-pvc"},
		Backend:               "s3",
		Bucket:                s3Bucket,
		Endpoint:              s3Endpoint,
		AccessKey:             s3AccessKey,
		SecretKey:             s3SecretKey,
		Name:                  backupName,
		IgnoreMounted:         true,
		NonRoot:               true,
		DeleteExtraneousFiles: true,
		NoCleanupOnFailure:    true,
		HelmValues:            infra.rcloneHelmVals,
		Logger:                slogt.New(t),
		Writer:                os.Stderr,
	}))
	assertPodStat(t, infra, destNS, "dest-pod", "/volume/secret.txt", "600 10000 10000 "+strings.TrimSpace(srcMtime))
	assertPodStat(t, infra, destNS, "dest-pod", "/volume/shared", "775 0 10000")
	assertPodStat(t, infra, destNS, "dest-pod", "/volume/shared/item.txt", "660 10000 10000")
	assertPodStat(t, infra, destNS, "dest-pod", "/volume/empty-dir", "750 10000 10000")
	assertPodStat(t, infra, destNS, "dest-pod", "/volume/lost+found", "700 0 0")
	_, err = execInPod(t.Context(), infra.cli, destNS, "dest-pod", "stat /volume/stale.txt")
	require.Error(t, err)
}

//nolint:thelper // subtest implementation, not a helper
func testRestorePosixMetadataRawRemote(t *testing.T, infra *backupTestInfra) {
	seedCmd := "echo -n RAW > /volume/raw.txt && chmod 0600 /volume/raw.txt && chown 1234:5678 /volume/raw.txt"
	ns := seedAndCreatePVC(t, infra, seedCmd)

	cfgPath := filepath.Join(t.TempDir(), "rclone.conf")
	cfgContent := fmt.Sprintf(
		"[raws3]\ntype = s3\nprovider = Other\nendpoint = %s\naccess_key_id = %s\nsecret_access_key = %s\n",
		s3Endpoint,
		s3AccessKey,
		s3SecretKey,
	)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o600))

	rawRemote := "raws3:" + s3Bucket + "/raw-" + ns

	require.NoError(t, pvmigrate.RunBackup(t.Context(), pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		RcloneConfigFile:   cfgPath,
		Remote:             rawRemote,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}))

	lsOutput := listBucketPath(t, infra, ns, "raw-"+ns+"/")
	assert.Contains(t, lsOutput, "raw.txt")
	assert.Contains(t, lsOutput, ".pv-migrate-posix.gz")

	require.NoError(t, provisionPod(t.Context(), infra.cli, ns, "dest-pvc", "dest-pod", ""))

	require.NoError(t, pvmigrate.RunRestore(t.Context(), pvmigrate.Restore{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "dest-pvc"},
		RcloneConfigFile:   cfgPath,
		Remote:             rawRemote,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}))

	assertPodStat(t, infra, ns, "dest-pod", "/volume/raw.txt", "600 1234 5678")

	_, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "stat /volume/.pv-migrate-posix.gz")
	require.Error(t, err)
}

//nolint:thelper // subtest implementation, not a helper
func testRestoreMissingPosixManifestWarns(t *testing.T, infra *backupTestInfra) {
	ns := seedAndCreatePVC(t, infra, backupSeedCmd())
	backupName := ns + "-legacy-backup"

	backup := pvmigrate.Backup{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "test-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slogt.New(t),
		Writer:             os.Stderr,
	}
	require.NoError(t, pvmigrate.RunBackup(t.Context(), backup))

	// Remove the POSIX manifest to simulate a backup written by an older version.
	runBucketVerifyJob(t, infra, ns, "deletefile", "test:"+s3Bucket+"/pv-migrate/"+backupName+"/.pv-migrate-posix.gz")

	require.NoError(t, provisionPod(t.Context(), infra.cli, ns, "dest-pvc", "dest-pod", ""))

	var logBuf bytes.Buffer

	restore := pvmigrate.Restore{
		PVC:                pvmigrate.PVC{Namespace: ns, Name: "dest-pvc"},
		Backend:            "s3",
		Bucket:             s3Bucket,
		Endpoint:           s3Endpoint,
		AccessKey:          s3AccessKey,
		SecretKey:          s3SecretKey,
		Name:               backupName,
		IgnoreMounted:      true,
		NoCleanupOnFailure: true,
		HelmValues:         infra.rcloneHelmVals,
		Logger:             slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, &logBuf), nil)),
		Writer:             os.Stderr,
	}
	require.NoError(t, pvmigrate.RunRestore(t.Context(), restore))

	output, err := execInPod(t.Context(), infra.cli, ns, "dest-pod", "cat "+backupTestDataPath)
	require.NoError(t, err)
	assert.Equal(t, backupTestDataContent, output)
	assert.Contains(t, logBuf.String(), "Backup has no POSIX metadata")
}

func assertPodStat(t *testing.T, infra *backupTestInfra, ns, podName, path, wantPrefix string) {
	t.Helper()

	got, err := execInPod(t.Context(), infra.cli, ns, podName, "stat -c '%a %u %g %Y' "+path)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(got), wantPrefix),
		"unexpected stat for %s: got %q, want prefix %q", path, strings.TrimSpace(got), wantPrefix)
}

// --- Verification helpers ---

// verifyBucketContents runs an rclone lsf job to check that the backup test file exists in the bucket.
func verifyBucketContents(t *testing.T, infra *backupTestInfra, ns, bucketPath string) {
	t.Helper()

	output := listBucketPath(t, infra, ns, bucketPath)
	assert.Contains(t, output, "backup_test.txt", "expected file not found in bucket at %s", bucketPath)
}

// verifyBucketEmpty checks that no objects exist at the given bucket path.
func verifyBucketEmpty(t *testing.T, infra *backupTestInfra, ns, bucketPath string) {
	t.Helper()

	output := listBucketPath(t, infra, ns, bucketPath)
	assert.Empty(t, output, "expected bucket path %s to be empty but got: %s", bucketPath, output)
}

// listBucketPath returns the rclone lsf output for the given path in the test bucket.
func listBucketPath(t *testing.T, infra *backupTestInfra, ns, bucketPath string) string {
	t.Helper()

	return runBucketVerifyJob(t, infra, ns, "lsf", "test:"+s3Bucket+"/"+bucketPath)
}

// readBucketFile returns the contents of a file in the test bucket.
func readBucketFile(t *testing.T, infra *backupTestInfra, ns, bucketPath string) string {
	t.Helper()

	return runBucketVerifyJob(t, infra, ns, "cat", "test:"+s3Bucket+"/"+bucketPath)
}

// runBucketVerifyJob creates a job in the given namespace that runs rclone against the test bucket,
// configured as the remote "test", and returns its output.
func runBucketVerifyJob(t *testing.T, infra *backupTestInfra, ns string, args ...string) string {
	t.Helper()

	ctx := t.Context()

	jobName := "verify-" + randomSuffix()

	job := createVerifyJob(t, infra, ns, jobName, args)

	// Wait for job completion
	err := k8s.WaitForJobCompletion(ctx, infra.cli.KubeClient, ns, job.Name,
		false, false, console.Palette{}, os.Stderr, slogt.New(t))
	require.NoError(t, err)

	// Get logs from the job pod
	pod, err := k8s.FindJobPod(ctx, infra.cli.KubeClient, job)
	require.NoError(t, err)

	logs, err := infra.cli.KubeClient.CoreV1().Pods(ns).GetLogs(pod.Name,
		&corev1.PodLogOptions{}).Stream(ctx)
	require.NoError(t, err)

	defer logs.Close()

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)

	for {
		n, readErr := logs.Read(tmp)
		buf = append(buf, tmp[:n]...)

		if readErr != nil {
			break
		}
	}

	return string(buf)
}

func createVerifyJob(t *testing.T, infra *backupTestInfra, ns, name string, args []string) *batchv1.Job {
	t.Helper()

	backoffLimit := int32(0)

	job := &batchv1.Job{
		Name:      name,
		Namespace: ns,
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "rclone",
							Image: verifyImage,
							// -q keeps rclone's notices out of the output the callers compare
							Args: append([]string{"-q"}, args...),
							Env: []corev1.EnvVar{
								{Name: "RCLONE_CONFIG_TEST_TYPE", Value: "s3"},
								{Name: "RCLONE_CONFIG_TEST_PROVIDER", Value: "Other"},
								{Name: "RCLONE_CONFIG_TEST_ENDPOINT", Value: s3Endpoint},
								{Name: "RCLONE_CONFIG_TEST_ACCESS_KEY_ID", Value: s3AccessKey},
								{Name: "RCLONE_CONFIG_TEST_SECRET_ACCESS_KEY", Value: s3SecretKey},
							},
						},
					},
				},
			},
		},
	}

	created, err := infra.cli.KubeClient.BatchV1().Jobs(ns).Create(t.Context(), job, metav1.CreateOptions{})
	require.NoError(t, err)

	return created
}

func waitForJobCompletion(t *testing.T, infra *backupTestInfra, ns string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	for {
		jobs, err := infra.cli.KubeClient.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/managed-by=Helm",
		})
		require.NoError(t, err)

		for i := range jobs.Items {
			if jobs.Items[i].Status.Succeeded > 0 {
				return
			}

			if jobs.Items[i].Status.Failed > 0 {
				t.Fatal("detached job failed")
			}
		}

		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for detached job to complete")
		case <-time.After(2 * time.Second):
		}
	}
}

func cleanupDetachedRelease(t *testing.T, infra *backupTestInfra, ns, migrationID string) {
	t.Helper()

	ac := new(action.Configuration)
	err := ac.Init(infra.cli.RESTClientGetter, ns, os.Getenv("HELM_DRIVER"))
	require.NoError(t, err)

	uninstall := action.NewUninstall(ac)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = 1 * time.Minute

	releaseName := "pv-migrate-" + migrationID + "-backup"
	_, _ = uninstall.Run(releaseName) // best-effort
}

func randomSuffix() string {
	return utilrand.String(5)
}
