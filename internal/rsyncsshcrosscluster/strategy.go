package rsyncsshcrosscluster

import (
	"errors"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

type RsyncSshCrossCluster struct {
}

func (r *RsyncSshCrossCluster) Cleanup(task task.Task) error {
	return errors.New("not implemented") // todo
}

func (r *RsyncSshCrossCluster) Name() string {
	return "rsync-ssh-cross-cluster"
}

func (r *RsyncSshCrossCluster) Priority() int {
	return 3000
}

func (r *RsyncSshCrossCluster) CanDo(task task.Task) bool {
	return false // todo
}

func (r *RsyncSshCrossCluster) Run(task task.Task) error {
	if !r.CanDo(task) {
		return errors.New("cannot do this task using this strategy")
	}
	return errors.New("not implemented") // todo
}
