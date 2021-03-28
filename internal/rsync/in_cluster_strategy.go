package rsync

import (
	"errors"
	"github.com/hashicorp/go-multierror"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

type InClusterStrategy struct {
}

func (r *InClusterStrategy) Cleanup(task *migration.Task) error {
	var result *multierror.Error
	err := Cleanup(task.Source.KubeClient, task.Id, task.Source.Claim.Namespace)
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = Cleanup(task.Dest.KubeClient, task.Id, task.Dest.Claim.Namespace)
	if err != nil {
		result = multierror.Append(result, err)
	}
	//goland:noinspection GoNilness
	return result.ErrorOrNil()
}

func (r *InClusterStrategy) Name() string {
	return "rsync-in-cluster"
}

func (r *InClusterStrategy) Priority() int {
	return 2000
}

func (r *InClusterStrategy) CanDo(task *migration.Task) bool {
	sameCluster := task.Source.KubeClient == task.Dest.KubeClient
	return sameCluster
}

func (r *InClusterStrategy) Run(task *migration.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}
	return MigrateViaRsync(task)
}
