package migration

import (
	"time"

	log "github.com/sirupsen/logrus"
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
	NoProgressBar         bool
	SourceMountReadOnly   bool
	KeyAlgorithm          string
	HelmTimeout           time.Duration
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
	Strategies            []string
	Logger                *log.Entry
	DestHostOverride      string
	LBSvcTimeout          time.Duration
}

type Migration struct {
	Chart      *chart.Chart
	Request    *Request
	Logger     *log.Entry
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
}

type Attempt struct {
	ID                    string
	HelmReleaseNamePrefix string
	Migration             *Migration
	Logger                *log.Entry
}
