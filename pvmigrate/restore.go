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

// Restore holds all configuration for restoring PVC data from bucket storage.
type Restore struct {
	// ID is an optional custom identifier. When empty, a petname-style identifier
	// is generated automatically.
	ID string

	// ImageTag is the Docker image tag for the rclone container.
	ImageTag string

	// ChartVersion overrides the embedded Helm chart version metadata.
	ChartVersion string

	// PVC is the destination PVC to restore data into.
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
	// Path is a subdirectory inside the PVC to restore into instead of the volume root.
	Path string

	// RcloneConfigFile is the path to a raw rclone.conf file (power-user escape hatch).
	RcloneConfigFile string
	// Remote is the remote spec for raw config mode (e.g., "myremote:bucket/path").
	Remote string

	// RcloneExtraArgs are extra flags appended to the rclone command after the built-in progress flags.
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

// RunRestore executes the restore.
func RunRestore(ctx context.Context, restore Restore) error {
	applyRestoreDefaults(&restore)

	if restore.ID != "" {
		if err := validateID(restore.ID); err != nil {
			return err
		}
	}

	req := toRestoreRequest(&restore)

	if err := bucketstorage.Run(ctx, req); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	return nil
}

func applyRestoreDefaults(restore *Restore) {
	if restore.Prefix == "" {
		restore.Prefix = DefaultPrefix
	}

	if restore.HelmTimeout == 0 {
		restore.HelmTimeout = defaultHelmTimeout
	}

	if restore.Writer == nil {
		restore.Writer = os.Stderr
	}

	if restore.Logger == nil {
		restore.Logger = slog.New(slog.DiscardHandler)
	}
}

func toRestoreRequest(restore *Restore) *bucketstorage.Request {
	return &bucketstorage.Request{
		ID:                    restore.ID,
		ImageTag:              restore.ImageTag,
		ChartVersion:          restore.ChartVersion,
		Direction:             rclone.DirectionRestore,
		KubeconfigPath:        restore.PVC.KubeconfigPath,
		Context:               restore.PVC.Context,
		Namespace:             restore.PVC.Namespace,
		PVCName:               restore.PVC.Name,
		IgnoreMounted:         restore.IgnoreMounted,
		NonRoot:               restore.NonRoot,
		Detach:                restore.Detach,
		NoCleanup:             restore.NoCleanup,
		NoCleanupOnFailure:    restore.NoCleanupOnFailure,
		Backend:               restore.Backend,
		Bucket:                restore.Bucket,
		Endpoint:              restore.Endpoint,
		Region:                restore.Region,
		AccessKey:             restore.AccessKey,
		SecretKey:             restore.SecretKey,
		StorageAccount:        restore.StorageAccount,
		StorageKey:            restore.StorageKey,
		GCSServiceAccountJSON: restore.GCSServiceAccountJSON,
		Name:                  restore.Name,
		Prefix:                restore.Prefix,
		Path:                  restore.Path,
		RcloneConfigFile:      restore.RcloneConfigFile,
		Remote:                restore.Remote,
		RcloneExtraArgs:       restore.RcloneExtraArgs,
		HelmTimeout:           restore.HelmTimeout,
		HelmValuesFiles:       restore.HelmValuesFiles,
		HelmValues:            restore.HelmValues,
		HelmFileValues:        restore.HelmFileValues,
		HelmStringValues:      restore.HelmStringValues,
		Writer:                restore.Writer,
		Logger:                restore.Logger,
	}
}
