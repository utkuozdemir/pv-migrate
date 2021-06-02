package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

type LbSvc struct {
}

func (r *LbSvc) Run(e *task.Execution) (bool, error) {
	doneCh := registerCleanupHook(e)
	defer cleanupAndReleaseHook(e, doneCh)
	return true, rsync.RunRsyncJobOverSSH(e, corev1.ServiceTypeLoadBalancer)
}
