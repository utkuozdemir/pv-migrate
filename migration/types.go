package migration

import (
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"helm.sh/helm/v3/pkg/chart"
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
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
	Strategies            []string
	Logger                *log.Entry
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
