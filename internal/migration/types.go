package migration

import (
	"io"
	"time"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

type PVCInfo struct {
	KubeconfigPath string
	Context        string
	Namespace      string
	Name           string
	Path           string
}

type Request struct {
	ID                    string
	ImageTag              string
	ChartVersion          string
	Source                PVCInfo
	Dest                  PVCInfo
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	IgnoreSizes           bool
	NoChown               bool
	Detach                bool
	Push                  bool
	NoCleanup             bool
	NoCleanupOnFailure    bool
	ShowProgressBar       bool
	SourceMountReadWrite  bool
	KeyAlgorithm          string
	SSHReverseTunnelPort  int
	HelmTimeout           time.Duration
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
	Strategies            []string
	DestHostOverride      string
	LoadBalancerTimeout   time.Duration
	NoCompress            bool
	NonRoot               bool
	RsyncExtraArgs        string
	Writer                io.Writer

	// StructuredLogs reports that the logger writes machine-readable records to
	// the same stream as Writer. Plain-text blocks are suppressed then, and the
	// same information is emitted as log records instead.
	StructuredLogs bool

	// ColorOutput colors the plain-text report blocks semantically. Set only
	// when the writer is a terminal and the logs are not machine-readable.
	ColorOutput bool
}

type Migration struct {
	Chart      *chart.Chart
	Request    *Request
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
}

type Attempt struct {
	ID                    string
	HelmReleaseNamePrefix string
	Migration             *Migration
	Detached              bool

	ReleaseNames []string

	// DiagnosticTargets records where this attempt actually installed something.
	// It is filled in at install time rather than reconstructed afterwards, so a
	// failure is only ever explained with resources this attempt created.
	DiagnosticTargets []DiagnosticTarget

	// Diagnostics is what the cluster reported about the attempt's resources,
	// collected on the failure path before cleanup removes them.
	Diagnostics string
}

// DiagnosticTarget is one Helm release and the PVC whose cluster and namespace
// it was installed into.
type DiagnosticTarget struct {
	Release string
	Info    *pvc.Info
}
