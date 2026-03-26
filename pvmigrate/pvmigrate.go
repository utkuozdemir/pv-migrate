package pvmigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
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
	// DefaultSSHReverseTunnelPort is the default port opened on the source pod's loopback
	// interface for the SSH reverse tunnel. Chosen below the IANA ephemeral range (49152–65535)
	// and below the typical Linux ephemeral range (32768–60999) to minimise collision risk.
	DefaultSSHReverseTunnelPort = 22000
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
	// ID is an optional custom migration identifier. When empty, a petname-style
	// identifier (e.g. "fuzzy-panda") is generated automatically. The value must be
	// lowercase alphanumeric with optional hyphens, not start or end with a hyphen,
	// and be at most maxIDLength characters long. The ID becomes part of Helm release
	// names and Kubernetes resource names, so it must be DNS-compatible.
	ID string

	// ImageTag is the default Docker image tag for the rsync and sshd containers.
	// When non-empty, it is injected as the lowest-priority Helm value so that
	// user overrides via HelmValues still take precedence.
	// Leave empty to use the chart's default (latest).
	ImageTag string

	// ChartVersion overrides the version and appVersion fields in the embedded
	// Helm chart metadata. When non-empty, this version is visible in helm list
	// output. Leave empty to use the chart's built-in version.
	ChartVersion string

	Source PVC
	Dest   PVC

	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	Detach                bool
	NoCleanup             bool
	ShowProgressBar       bool
	SourceMountReadWrite  bool
	NoCompress            bool
	NonRoot               bool
	RsyncExtraArgs        string

	KeyAlgorithm         KeyAlgorithm
	SSHReverseTunnelPort int
	Strategies           []Strategy
	DestHostOverride     string
	HelmTimeout          time.Duration
	LoadBalancerTimeout  time.Duration
	HelmValuesFiles      []string
	HelmValues           []string
	HelmFileValues       []string
	HelmStringValues     []string

	Writer io.Writer
	Logger *slog.Logger
}

// maxIDLength limits the migration ID so that the longest possible Kubernetes
// resource name stays within the 63-character DNS label limit.
// Worst case: "pv-migrate-" (11) + <id> + "-loadbalancer" (13) + "-dest" (5) + "-rsync" (6) = 35 overhead.
const maxIDLength = 63 - 35

var validIDRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateID checks that the given migration ID is valid for use in Kubernetes
// resource names. It must be lowercase alphanumeric with optional hyphens,
// must not start or end with a hyphen, and be at most maxIDLength characters.
func validateID(id string) error {
	if len(id) == 0 {
		return errors.New("migration ID must not be empty")
	}

	if len(id) > maxIDLength {
		return fmt.Errorf("migration ID %q is too long (%d chars), maximum is %d", id, len(id), maxIDLength)
	}

	if !validIDRegex.MatchString(id) {
		return fmt.Errorf("migration ID %q is invalid: must be lowercase alphanumeric with optional hyphens, "+
			"and must not start or end with a hyphen", id)
	}

	return nil
}

// Run executes the migration.
func Run(ctx context.Context, migration Migration) error {
	migration.ApplyDefaults()

	if migration.ID != "" {
		if err := validateID(migration.ID); err != nil {
			return err
		}
	}

	if p := migration.SSHReverseTunnelPort; p < 1 || p > 65535 {
		return fmt.Errorf("invalid ssh-reverse-tunnel-port %d: must be between 1 and 65535", p)
	}

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

	if m.SSHReverseTunnelPort == 0 {
		m.SSHReverseTunnelPort = DefaultSSHReverseTunnelPort
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
		ID:           mig.ID,
		ImageTag:     mig.ImageTag,
		ChartVersion: mig.ChartVersion,
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
		Detach:                mig.Detach,
		NoCleanup:             mig.NoCleanup,
		ShowProgressBar:       mig.ShowProgressBar,
		SourceMountReadWrite:  mig.SourceMountReadWrite,
		NoCompress:            mig.NoCompress,
		NonRoot:               mig.NonRoot,
		RsyncExtraArgs:        mig.RsyncExtraArgs,
		KeyAlgorithm:          string(mig.KeyAlgorithm),
		SSHReverseTunnelPort:  mig.SSHReverseTunnelPort,
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
