package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

type Svc struct {
}

func (r *Svc) canDo(t *task.Task) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.KubeClient == d.KubeClient
	return sameCluster
}

func (r *Svc) Run(e *task.Execution) (bool, error) {
	if !r.canDo(e.Task) {
		return false, nil
	}

	doneCh := registerCleanupHook(e)
	defer cleanupAndReleaseHook(e, doneCh)
	return true, rsync.RunRsyncJobOverSSH(e, corev1.ServiceTypeClusterIP)
}
