package task

import (
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
)

type Task struct {
	Migration  *migration.Migration
	LogFields  log.Fields
	SourceInfo *pvc.Info
	DestInfo   *pvc.Info
	ID         string
}
