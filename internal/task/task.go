package task

import (
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
	"helm.sh/helm/v3/pkg/chart"
)

type Task struct {
	Chart      *chart.Chart
	Migration  *migration.Migration
	Logger     *log.Entry
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
}

type Execution struct {
	ID                    string
	HelmReleaseNamePrefix string
	Task                  *Task
	Logger                *log.Entry
}
