package rsyncsshcrosscluster

import (
	"errors"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

type RsyncSSHCrossCluster struct {
}

func (r *RsyncSSHCrossCluster) Cleanup(task task.Task) error {
	return errors.New("not implemented") // todo
}

func (r *RsyncSSHCrossCluster) Name() string {
	return "rsync-ssh-cross-cluster"
}

func (r *RsyncSSHCrossCluster) Priority() int {
	return 3000
}

func (r *RsyncSSHCrossCluster) CanDo(task task.Task) bool {
	return false // todo
}

func (r *RsyncSSHCrossCluster) Run(task task.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}
	return errors.New("not implemented") // todo
}
