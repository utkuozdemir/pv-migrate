package task

import (
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
)

type Task struct {
	Migration  *migration.Migration
	Logger     *log.Entry
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
}

type Execution struct {
	ID     string
	Task   *Task
	Logger *log.Entry
}
