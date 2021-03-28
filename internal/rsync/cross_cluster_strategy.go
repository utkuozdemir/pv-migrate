package rsync

import (
	"errors"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

type CrossClusterStrategy struct {
}

func (r *CrossClusterStrategy) Cleanup(task *migration.Task) error {
	// TODO
	return errors.New("not implemented")
}

func (r *CrossClusterStrategy) Name() string {
	return "rsync-cross-cluster"
}

func (r *CrossClusterStrategy) Priority() int {
	return 3000
}

func (r *CrossClusterStrategy) CanDo(task *migration.Task) bool {
	return true
}

func (r *CrossClusterStrategy) Run(task *migration.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}

	// TODO
	return errors.New("not implemented")
}
