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
	Source                PVCInfo
	Dest                  PVCInfo
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	NoCleanup             bool
	ShowProgressBar       bool
	SourceMountReadWrite  bool
	KeyAlgorithm          string
	HelmTimeout           time.Duration
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
	Strategies            []string
	DestHostOverride      string
	LoadBalancerTimeout   time.Duration
	NoCompress            bool
	Writer                io.Writer
}

type Migration struct {
	Chart      *chart.Chart
	Request    *Request
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
}

// SourceEndpoint holds information about a shared source endpoint
// that can be reused across multiple transfers in batch mode.
type SourceEndpoint struct {
	// Address is the SSH target host (already formatted, e.g. with brackets for IPv6).
	Address string
	// ReleaseName is the Helm release name of the shared source sshd endpoint.
	ReleaseName string
	// SrcMountPath is the mount path of this specific source PVC on the shared sshd pod.
	SrcMountPath string
	// PrivateKey is the SSH private key corresponding to the shared source's public key.
	PrivateKey string
	// KeyAlgorithm is the SSH key algorithm used (e.g. "ed25519").
	KeyAlgorithm string
}

type Attempt struct {
	ID                    string
	HelmReleaseNamePrefix string
	Migration             *Migration
	// SourceEndpoint, if set, indicates a shared source endpoint to reuse.
	// When set, the LoadBalancer strategy skips installing a source endpoint
	// and uses this pre-existing one instead.
	SourceEndpoint *SourceEndpoint
}
