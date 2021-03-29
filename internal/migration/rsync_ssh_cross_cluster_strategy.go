package migration

import (
	"errors"
)

type RsyncSshCrossClusterStrategy struct {
}

func (r *RsyncSshCrossClusterStrategy) Cleanup(task Task) error {
	return errors.New("not implemented") // todo
}

func (r *RsyncSshCrossClusterStrategy) Name() string {
	return "rsync-ssh-cross-cluster"
}

func (r *RsyncSshCrossClusterStrategy) Priority() int {
	return 3000
}

func (r *RsyncSshCrossClusterStrategy) CanDo(task Task) bool {
	return false // todo
}

func (r *RsyncSshCrossClusterStrategy) Run(task Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}
	return errors.New("not implemented") // todo
}
