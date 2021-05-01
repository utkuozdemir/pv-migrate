package strategy

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

const (
	Mnt2Strategy  = "mnt2"
	SvcStrategy   = "svc"
	LbSvcStrategy = "lbsvc"
)

var (
	DefaultStrategies = []string{Mnt2Strategy, SvcStrategy, LbSvcStrategy}

	nameToStrategy = map[string]Strategy{
		Mnt2Strategy:  &Mnt2{},
		SvcStrategy:   &Svc{},
		LbSvcStrategy: &LbSvc{},
	}
)

type Strategy interface {
	// Run executes the migration for the given task.
	//
	// This is the actual implementation of the migration.
	Run(task task.Task) (bool, error)
}

func GetStrategiesMapForNames(names []string) (map[string]Strategy, error) {
	sts := make(map[string]Strategy)
	for _, name := range names {
		s, ok := nameToStrategy[name]
		if !ok {
			return nil, fmt.Errorf("strategy not found: %s", name)
		}

		sts[name] = s
	}
	return sts, nil
}

func cleanup(task task.Task) {
	log.Info("Cleaning up")
	migrationJob := task.Job()
	var result *multierror.Error
	err := k8s.CleanupForID(migrationJob.Source().KubeClient(), migrationJob.Source().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = k8s.CleanupForID(migrationJob.Dest().KubeClient(), migrationJob.Dest().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}

	//goland:noinspection GoNilness
	err = result.ErrorOrNil()
	if err != nil {
		log.WithError(err).Warn("Cleanup failed, you might want to clean up manually")
	}
}
