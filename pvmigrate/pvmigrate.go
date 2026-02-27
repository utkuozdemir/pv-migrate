package pvmigrate

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/migrator"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

// Strategy identifies a migration strategy.
type Strategy string

const (
	Mount        Strategy = "mount"
	ClusterIP    Strategy = "clusterip"
	LoadBalancer Strategy = "loadbalancer"
	NodePort     Strategy = "nodeport"
	Local        Strategy = "local"
)

// KeyAlgorithm identifies an SSH key algorithm.
type KeyAlgorithm string

const (
	Ed25519 KeyAlgorithm = "ed25519"
	RSA     KeyAlgorithm = "rsa"
)

const (
	defaultHelmTimeout         = 1 * time.Minute
	defaultLoadBalancerTimeout = 2 * time.Minute
	defaultPath                = "/"
)

var (
	DefaultStrategies = []Strategy{Mount, ClusterIP, LoadBalancer}
	AllStrategies     = []Strategy{Mount, ClusterIP, LoadBalancer, NodePort, Local}
	KeyAlgorithms     = []KeyAlgorithm{RSA, Ed25519}
)

// PVC identifies a PersistentVolumeClaim to migrate data from or to.
type PVC struct {
	KubeconfigPath string
	Context        string
	Namespace      string
	Name           string
	Path           string
}

// Migration holds all configuration for a PVC data migration.
type Migration struct {
	// ImageTag is the default Docker image tag for the rsync and sshd containers.
	// When non-empty, it is injected as the lowest-priority Helm value so that
	// user overrides via HelmValues still take precedence.
	// Leave empty to use the chart's default (latest).
	ImageTag string

	Source PVC
	Dest   PVC

	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	NoCleanup             bool
	ShowProgressBar       bool
	SourceMountReadWrite  bool
	NoCompress            bool

	KeyAlgorithm        KeyAlgorithm
	Strategies          []Strategy
	DestHostOverride    string
	HelmTimeout         time.Duration
	LoadBalancerTimeout time.Duration
	HelmValuesFiles     []string
	HelmValues          []string
	HelmFileValues      []string
	HelmStringValues    []string

	Writer io.Writer
	Logger *slog.Logger
}

// Run executes the migration.
func Run(ctx context.Context, migration Migration) error {
	migration.ApplyDefaults()
	req := toInternalRequest(&migration)

	if err := migrator.New().Run(ctx, req, migration.Logger); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// ApplyDefaults fills zero-value fields with sensible defaults.
// A zero-value Migration is fully functional after calling ApplyDefaults.
func (m *Migration) ApplyDefaults() {
	if m.Source.Path == "" {
		m.Source.Path = defaultPath
	}

	if m.Dest.Path == "" {
		m.Dest.Path = defaultPath
	}

	if len(m.Strategies) == 0 {
		m.Strategies = DefaultStrategies
	}

	if m.KeyAlgorithm == "" {
		m.KeyAlgorithm = Ed25519
	}

	if m.HelmTimeout == 0 {
		m.HelmTimeout = defaultHelmTimeout
	}

	if m.LoadBalancerTimeout == 0 {
		m.LoadBalancerTimeout = defaultLoadBalancerTimeout
	}

	if m.Writer == nil {
		m.Writer = os.Stderr
	}

	if m.Logger == nil {
		m.Logger = slog.New(slog.DiscardHandler)
	}
}

func toInternalRequest(mig *Migration) *migration.Request {
	return &migration.Request{
		ImageTag: mig.ImageTag,
		Source: migration.PVCInfo{
			KubeconfigPath: mig.Source.KubeconfigPath,
			Context:        mig.Source.Context,
			Namespace:      mig.Source.Namespace,
			Name:           mig.Source.Name,
			Path:           mig.Source.Path,
		},
		Dest: migration.PVCInfo{
			KubeconfigPath: mig.Dest.KubeconfigPath,
			Context:        mig.Dest.Context,
			Namespace:      mig.Dest.Namespace,
			Name:           mig.Dest.Name,
			Path:           mig.Dest.Path,
		},
		DeleteExtraneousFiles: mig.DeleteExtraneousFiles,
		IgnoreMounted:         mig.IgnoreMounted,
		NoChown:               mig.NoChown,
		NoCleanup:             mig.NoCleanup,
		ShowProgressBar:       mig.ShowProgressBar,
		SourceMountReadWrite:  mig.SourceMountReadWrite,
		NoCompress:            mig.NoCompress,
		KeyAlgorithm:          string(mig.KeyAlgorithm),
		Strategies:            util.ConvertStrings[string](mig.Strategies),
		DestHostOverride:      mig.DestHostOverride,
		HelmTimeout:           mig.HelmTimeout,
		LoadBalancerTimeout:   mig.LoadBalancerTimeout,
		HelmValuesFiles:       mig.HelmValuesFiles,
		HelmValues:            mig.HelmValues,
		HelmFileValues:        mig.HelmFileValues,
		HelmStringValues:      mig.HelmStringValues,
		Writer:                mig.Writer,
	}
}
