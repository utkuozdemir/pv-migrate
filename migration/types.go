package migration

import (
	"time"

	"helm.sh/helm/v3/pkg/chart"

	"github.com/utkuozdemir/pv-migrate/pvc"
)

type PVCInfo struct {
	KubeconfigPath string
	Context        string
	Namespace      string
	Name           string
	Path           string
}

type Request struct {
	Source                *PVCInfo
	Dest                  *PVCInfo
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	SkipCleanup           bool
	NoProgressBar         bool
	SourceMountReadOnly   bool
	KeyAlgorithm          string
	HelmTimeout           time.Duration
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
	Strategies            []string
	DestHostOverride      string
	LBSvcTimeout          time.Duration
	Compress              bool
	NodePortPort          int // Custom port for NodePort strategy (30000-32767)
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
}
