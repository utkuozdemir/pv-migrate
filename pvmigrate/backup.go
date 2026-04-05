package pvmigrate

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/utkuozdemir/pv-migrate/internal/bucketstorage"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

// Backup holds all configuration for a PVC backup to bucket storage.
type Backup struct {
	// ID is an optional custom identifier. When empty, a petname-style identifier
	// is generated automatically.
	ID string

	// ImageTag is the Docker image tag for the rclone container.
	// When non-empty, it is injected as the lowest-priority Helm value.
	ImageTag string

	// ChartVersion overrides the embedded Helm chart version metadata.
	ChartVersion string

	PVC PVC

	// Backend is the storage backend: "s3", "azure", or "gcs".
	Backend string
	// Bucket is the bucket (or container) name.
	Bucket string

	// S3-specific options
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string

	// Azure-specific options
	StorageAccount string
	StorageKey     string

	// GCS-specific options
	GCSServiceAccountJSON string

	// Name is the backup identity in the bucket. Required.
	Name string
	// Prefix is the global prefix in the bucket (default: pv-migrate).
	Prefix string
	// Path is a subdirectory inside the PVC to back up instead of the entire volume.
	Path string

	// RcloneConfigFile is the path to a raw rclone.conf file (power-user escape hatch).
	RcloneConfigFile string
	// Remote is the remote spec for raw config mode (e.g., "myremote:bucket/path").
	Remote string

	// RcloneExtraArgs are extra flags appended to the rclone command.
	RcloneExtraArgs string

	IgnoreMounted      bool
	NonRoot            bool
	Detach             bool
	NoCleanup          bool
	NoCleanupOnFailure bool

	HelmTimeout      time.Duration
	HelmValuesFiles  []string
	HelmValues       []string
	HelmFileValues   []string
	HelmStringValues []string

	Writer io.Writer
	Logger *slog.Logger
}

// RunBackup executes the backup.
func RunBackup(ctx context.Context, backup Backup) error {
	applyBackupDefaults(&backup)

	if backup.ID != "" {
		if err := validateID(backup.ID); err != nil {
			return err
		}
	}

	req := toBackupRequest(&backup, rclone.DirectionBackup)

	if err := bucketstorage.Run(ctx, req); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return nil
}

func applyBackupDefaults(backup *Backup) {
	if backup.Prefix == "" {
		backup.Prefix = DefaultPrefix
	}

	if backup.HelmTimeout == 0 {
		backup.HelmTimeout = defaultHelmTimeout
	}

	if backup.Writer == nil {
		backup.Writer = os.Stderr
	}

	if backup.Logger == nil {
		backup.Logger = slog.New(slog.DiscardHandler)
	}
}

func toBackupRequest(backup *Backup, direction string) *bucketstorage.Request {
	return &bucketstorage.Request{
		ID:                    backup.ID,
		ImageTag:              backup.ImageTag,
		ChartVersion:          backup.ChartVersion,
		Direction:             direction,
		KubeconfigPath:        backup.PVC.KubeconfigPath,
		Context:               backup.PVC.Context,
		Namespace:             backup.PVC.Namespace,
		PVCName:               backup.PVC.Name,
		IgnoreMounted:         backup.IgnoreMounted,
		NonRoot:               backup.NonRoot,
		Detach:                backup.Detach,
		NoCleanup:             backup.NoCleanup,
		NoCleanupOnFailure:    backup.NoCleanupOnFailure,
		Backend:               backup.Backend,
		Bucket:                backup.Bucket,
		Endpoint:              backup.Endpoint,
		Region:                backup.Region,
		AccessKey:             backup.AccessKey,
		SecretKey:             backup.SecretKey,
		StorageAccount:        backup.StorageAccount,
		StorageKey:            backup.StorageKey,
		GCSServiceAccountJSON: backup.GCSServiceAccountJSON,
		Name:                  backup.Name,
		Prefix:                backup.Prefix,
		Path:                  backup.Path,
		RcloneConfigFile:      backup.RcloneConfigFile,
		Remote:                backup.Remote,
		RcloneExtraArgs:       backup.RcloneExtraArgs,
		HelmTimeout:           backup.HelmTimeout,
		HelmValuesFiles:       backup.HelmValuesFiles,
		HelmValues:            backup.HelmValues,
		HelmFileValues:        backup.HelmFileValues,
		HelmStringValues:      backup.HelmStringValues,
		Writer:                backup.Writer,
		Logger:                backup.Logger,
	}
}
