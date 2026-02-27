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

// Transfer defines a single source→dest PVC pair for batch migration.
type Transfer struct {
	Source PVC
	Dest   PVC
}

// BatchMigration holds all configuration for a batch PVC data migration.
// In batch mode, transfers that share a source namespace are optimised to
// reuse a single LoadBalancer endpoint instead of creating one per transfer.
type BatchMigration struct {
	Transfers []Transfer

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

// ApplyDefaults fills zero-value fields with sensible defaults.
func (b *BatchMigration) ApplyDefaults() {
	for i := range b.Transfers {
		if b.Transfers[i].Source.Path == "" {
			b.Transfers[i].Source.Path = defaultPath
		}

		if b.Transfers[i].Dest.Path == "" {
			b.Transfers[i].Dest.Path = defaultPath
		}
	}

	if len(b.Strategies) == 0 {
		b.Strategies = DefaultStrategies
	}

	if b.KeyAlgorithm == "" {
		b.KeyAlgorithm = Ed25519
	}

	if b.HelmTimeout == 0 {
		b.HelmTimeout = defaultHelmTimeout
	}

	if b.LoadBalancerTimeout == 0 {
		b.LoadBalancerTimeout = defaultLoadBalancerTimeout
	}

	if b.Writer == nil {
		b.Writer = os.Stderr
	}

	if b.Logger == nil {
		b.Logger = slog.New(slog.DiscardHandler)
	}
}

// RunBatch executes a batch migration.
func RunBatch(ctx context.Context, batch BatchMigration) error {
	batch.ApplyDefaults()

	requests := make([]*migration.Request, 0, len(batch.Transfers))
	for i := range batch.Transfers {
		req := toBatchInternalRequest(&batch, &batch.Transfers[i])
		requests = append(requests, req)
	}

	if err := migrator.New().RunBatch(ctx, requests, batch.Logger); err != nil {
		return fmt.Errorf("batch migration failed: %w", err)
	}

	return nil
}

func toBatchInternalRequest(batch *BatchMigration, t *Transfer) *migration.Request {
	return &migration.Request{
		Source: migration.PVCInfo{
			KubeconfigPath: t.Source.KubeconfigPath,
			Context:        t.Source.Context,
			Namespace:      t.Source.Namespace,
			Name:           t.Source.Name,
			Path:           t.Source.Path,
		},
		Dest: migration.PVCInfo{
			KubeconfigPath: t.Dest.KubeconfigPath,
			Context:        t.Dest.Context,
			Namespace:      t.Dest.Namespace,
			Name:           t.Dest.Name,
			Path:           t.Dest.Path,
		},
		DeleteExtraneousFiles: batch.DeleteExtraneousFiles,
		IgnoreMounted:         batch.IgnoreMounted,
		NoChown:               batch.NoChown,
		NoCleanup:             batch.NoCleanup,
		ShowProgressBar:       batch.ShowProgressBar,
		SourceMountReadWrite:  batch.SourceMountReadWrite,
		NoCompress:            batch.NoCompress,
		KeyAlgorithm:          string(batch.KeyAlgorithm),
		Strategies:            util.ConvertStrings[string](batch.Strategies),
		DestHostOverride:      batch.DestHostOverride,
		HelmTimeout:           batch.HelmTimeout,
		LoadBalancerTimeout:   batch.LoadBalancerTimeout,
		HelmValuesFiles:       batch.HelmValuesFiles,
		HelmValues:            batch.HelmValues,
		HelmFileValues:        batch.HelmFileValues,
		HelmStringValues:      batch.HelmStringValues,
		Writer:                batch.Writer,
	}
}
