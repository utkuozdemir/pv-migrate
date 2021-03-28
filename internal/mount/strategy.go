package mount

import (
	"errors"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

type SourcePvcMountStrategy struct {
}

func (r *SourcePvcMountStrategy) Cleanup(task *migration.Task) error {
	// TODO
	return errors.New("not implemented")
}

func (r *SourcePvcMountStrategy) Name() string {
	return "source-pvc-mount"
}

func (r *SourcePvcMountStrategy) Priority() int {
	return 1000
}

func (r *SourcePvcMountStrategy) CanDo(task *migration.Task) bool {
	sameCluster := task.Source.KubeClient == task.Dest.KubeClient
	sameNamespace := task.Source.Claim.Namespace == task.Dest.Claim.Namespace
	return sameCluster && sameNamespace
}

func (r *SourcePvcMountStrategy) Run(task *migration.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}
	// TODO
	return errors.New("not implemented")
}
