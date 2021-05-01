package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

type LbSvc struct {
}

func (r *LbSvc) Run(task task.Task) (bool, error) {
	defer cleanup(task)
	return true, rsync.RunRsyncJobOverSsh(task, corev1.ServiceTypeLoadBalancer)
}
