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
	Source PVC
	Dest   PVC

	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	NoCleanup           bool
	ShowProgressBar       bool
	SourceMountReadOnly   bool
	Compress              bool

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

// NewMigration creates a Migration with sensible defaults.
func NewMigration() Migration {
	return Migration{
		Source: PVC{
			Path: defaultPath,
		},
		Dest: PVC{
			Path: defaultPath,
		},
		Compress:            true,
		SourceMountReadOnly: true,
		KeyAlgorithm:        Ed25519,
		Strategies:          DefaultStrategies,
		HelmTimeout:         defaultHelmTimeout,
		LoadBalancerTimeout: defaultLoadBalancerTimeout,
		Writer:              os.Stderr,
		Logger:              slog.New(slog.DiscardHandler),
	}
}

// Run executes the migration.
func Run(ctx context.Context, mig *Migration) error {
	mig.applyDefaults()
	req := toInternalRequest(mig)

	if err := migrator.New().Run(ctx, req, mig.Logger); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

func (m *Migration) applyDefaults() {
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
		NoCleanup:           mig.NoCleanup,
		ShowProgressBar:       mig.ShowProgressBar,
		SourceMountReadOnly:   mig.SourceMountReadOnly,
		Compress:              mig.Compress,
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
