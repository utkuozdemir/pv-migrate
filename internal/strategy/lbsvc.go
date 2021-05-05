package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

type LbSvc struct {
}

func (r *LbSvc) Run(t *task.Task) (bool, error) {
	defer cleanup(t)
	return true, rsync.RunRsyncJobOverSSH(t, corev1.ServiceTypeLoadBalancer)
}
