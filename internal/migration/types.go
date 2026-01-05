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
	ImageTag              string
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

type Attempt struct {
	ID                    string
	HelmReleaseNamePrefix string
	Migration             *Migration
}
