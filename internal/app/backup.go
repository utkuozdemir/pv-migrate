package app

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

const (
	FlagBackend               = "backend"
	FlagBucket                = "bucket"
	FlagEndpoint              = "endpoint"
	FlagRegion                = "region"
	FlagAccessKey             = "access-key"
	FlagSecretKey             = "secret-key"
	FlagStorageAccount        = "storage-account"
	FlagStorageKey            = "storage-key"
	FlagGCSServiceAccountFile = "gcs-service-account-file"
	FlagName                  = "name"
	FlagPrefix                = "prefix"
	FlagRcloneConfig          = "rclone-config"
	FlagRemote                = "remote"
	FlagPath                  = "path"
	FlagRcloneExtraArgs       = "rclone-extra-args"

	defaultHelmTimeout = 1 * time.Minute
)

//nolint:dupl
func buildBackupCmd(logger **slog.Logger) (*cobra.Command, error) {
	var backup pvmigrate.Backup

	cmd := &cobra.Command{
		Use:   "backup --source <pvc-name> --backend <backend> --bucket <bucket>",
		Short: "Back up a PVC to bucket storage",
		Long: "Back up data from a Kubernetes PersistentVolumeClaim to S3-compatible, " +
			"Azure Blob, or GCS bucket storage using rclone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackup(cmd, &backup, *logger)
		},
	}

	if err := setBackupRestoreFlags(cmd, &backup.PVC, &backup.ID, &backup.IgnoreMounted, &backup.NonRoot,
		&backup.Detach, &backup.NoCleanup, &backup.NoCleanupOnFailure,
		&backup.HelmTimeout, &backup.HelmValuesFiles, &backup.HelmValues,
		&backup.HelmStringValues, &backup.HelmFileValues); err != nil {
		return nil, err
	}

	if err := setBucketStorageFlags(cmd, &backup.Backend, &backup.Bucket, &backup.Endpoint, &backup.Region,
		&backup.AccessKey, &backup.SecretKey, &backup.StorageAccount, &backup.StorageKey,
		&backup.Name, &backup.Prefix, &backup.Path, &backup.RcloneExtraArgs); err != nil {
		return nil, err
	}

	setRawConfigFlags(cmd, &backup.RcloneConfigFile, &backup.Remote)

	return cmd, nil
}

func runBackup(cmd *cobra.Command, backup *pvmigrate.Backup, logger *slog.Logger) error {
	ctx := cmd.Context()
	backup.Writer = cmd.ErrOrStderr()
	backup.Logger = logger

	if err := readGCSServiceAccountFile(cmd, &backup.GCSServiceAccountJSON); err != nil {
		return err
	}

	logger.Info("📦 Starting backup")

	if err := pvmigrate.RunBackup(ctx, *backup); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return nil
}

//nolint:dupl
func buildRestoreCmd(logger **slog.Logger) (*cobra.Command, error) {
	var restore pvmigrate.Restore

	cmd := &cobra.Command{
		Use:   "restore --source <pvc-name> --backend <backend> --bucket <bucket>",
		Short: "Restore a PVC from bucket storage",
		Long: "Restore data from S3-compatible, Azure Blob, or GCS bucket storage " +
			"to a Kubernetes PersistentVolumeClaim using rclone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestore(cmd, &restore, *logger)
		},
	}

	if err := setBackupRestoreFlags(cmd, &restore.PVC, &restore.ID, &restore.IgnoreMounted, &restore.NonRoot,
		&restore.Detach, &restore.NoCleanup, &restore.NoCleanupOnFailure,
		&restore.HelmTimeout, &restore.HelmValuesFiles, &restore.HelmValues,
		&restore.HelmStringValues, &restore.HelmFileValues); err != nil {
		return nil, err
	}

	if err := setBucketStorageFlags(cmd, &restore.Backend, &restore.Bucket, &restore.Endpoint, &restore.Region,
		&restore.AccessKey, &restore.SecretKey, &restore.StorageAccount, &restore.StorageKey,
		&restore.Name, &restore.Prefix, &restore.Path, &restore.RcloneExtraArgs); err != nil {
		return nil, err
	}

	setRawConfigFlags(cmd, &restore.RcloneConfigFile, &restore.Remote)

	return cmd, nil
}

func runRestore(cmd *cobra.Command, restore *pvmigrate.Restore, logger *slog.Logger) error {
	ctx := cmd.Context()
	restore.Writer = cmd.ErrOrStderr()
	restore.Logger = logger

	if err := readGCSServiceAccountFile(cmd, &restore.GCSServiceAccountJSON); err != nil {
		return err
	}

	logger.Info("📥 Starting restore")

	if err := pvmigrate.RunRestore(ctx, *restore); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	return nil
}

func readGCSServiceAccountFile(cmd *cobra.Command, target *string) error {
	saFile, err := cmd.Flags().GetString(FlagGCSServiceAccountFile)
	if err != nil {
		return fmt.Errorf("failed to get flag %s: %w", FlagGCSServiceAccountFile, err)
	}

	if saFile == "" {
		return nil
	}

	data, err := os.ReadFile(saFile)
	if err != nil {
		return fmt.Errorf("failed to read GCS service account file %s: %w", saFile, err)
	}

	*target = string(data)

	return nil
}

func setBackupRestoreFlags(
	cmd *cobra.Command,
	pvc *pvmigrate.PVC,
	id *string,
	ignoreMounted, nonRoot, detach, noCleanup, noCleanupOnFailure *bool,
	helmTimeout *time.Duration,
	helmValuesFiles, helmValues, helmStringValues, helmFileValues *[]string,
) error {
	flags := cmd.Flags()

	flags.StringVarP(&pvc.KubeconfigPath, FlagSourceKubeconfig, "k", "", "Path to the kubeconfig file")
	flags.StringVarP(&pvc.Context, FlagSourceContext, "c", "", "Kubernetes context to use")
	flags.StringVarP(&pvc.Namespace, FlagSourceNamespace, "n", "", "Namespace of the PVC")
	flags.StringVar(&pvc.Name, FlagSource, "", "PVC name")

	if err := cmd.MarkFlagRequired(FlagSource); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagSource, err)
	}

	flags.StringVar(id, FlagID, "", "Custom operation ID (lowercase alphanumeric with optional hyphens, max 28 chars)")
	flags.BoolVarP(ignoreMounted, FlagIgnoreMounted, "i", false, "Do not fail if the PVC is mounted")
	flags.BoolVar(nonRoot, FlagNonRoot, false, "Run rclone container as non-root")
	flags.BoolVar(detach, FlagDetach, false, "Detach after the rclone job starts running")
	flags.BoolVarP(noCleanup, FlagNoCleanup, "x", false, "Do not clean up after the operation")
	flags.BoolVar(noCleanupOnFailure, FlagNoCleanupOnFailure, false,
		"Skip cleanup if the operation fails, leaving resources for inspection")
	flags.DurationVarP(helmTimeout, FlagHelmTimeout, "t", defaultHelmTimeout, "Helm install/uninstall timeout")
	flags.StringSliceVarP(helmValuesFiles, FlagHelmValues, "f", nil,
		"Additional Helm values files (YAML file or URL, can specify multiple)")
	flags.StringSliceVar(helmValues, FlagHelmSet, nil, "Additional Helm values (key1=val1,key2=val2)")
	flags.StringSliceVar(helmStringValues, FlagHelmSetString, nil,
		"Additional Helm string values (key1=val1,key2=val2)")
	flags.StringSliceVar(helmFileValues, FlagHelmSetFile, nil,
		"Additional Helm values from files (key1=path1,key2=path2)")

	return nil
}

func setBucketStorageFlags(
	cmd *cobra.Command,
	backend, bucket, endpoint, region, accessKey, secretKey, storageAccount, storageKey,
	name, prefix, pvcPath, rcloneExtraArgs *string,
) error {
	flags := cmd.Flags()

	flags.StringVar(backend, FlagBackend, "", "Storage backend: s3, azure, or gcs")
	flags.StringVar(bucket, FlagBucket, "", "Bucket (or container) name")
	flags.StringVar(endpoint, FlagEndpoint, "", "S3-compatible endpoint URL")
	flags.StringVar(region, FlagRegion, "", "S3 region")
	flags.StringVar(accessKey, FlagAccessKey, "", "S3 access key")
	flags.StringVar(secretKey, FlagSecretKey, "", "S3 secret key")
	flags.StringVar(storageAccount, FlagStorageAccount, "", "Azure storage account name")
	flags.StringVar(storageKey, FlagStorageKey, "", "Azure storage account key")
	flags.String(FlagGCSServiceAccountFile, "",
		"Path to GCS service account JSON file")
	flags.StringVar(name, FlagName, "", "Backup name (identity in the bucket)")

	if err := cmd.MarkFlagRequired(FlagName); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagName, err)
	}

	flags.StringVar(prefix, FlagPrefix, pvmigrate.DefaultPrefix, "Global prefix in the bucket")
	flags.StringVarP(pvcPath, FlagPath, "p", "", "Subdirectory inside the PVC to back up or restore")
	flags.StringVar(rcloneExtraArgs, FlagRcloneExtraArgs, "",
		"Extra rclone flags appended to the rclone command (use at your own risk)")

	return nil
}

func setRawConfigFlags(cmd *cobra.Command, rcloneConfig, remote *string) {
	flags := cmd.Flags()

	flags.StringVar(rcloneConfig, FlagRcloneConfig, "",
		"Path to a raw rclone.conf file (overrides --backend and credential flags)")
	flags.StringVar(remote, FlagRemote, "",
		"Remote spec for raw config mode (e.g., myremote:bucket/path)")
}
