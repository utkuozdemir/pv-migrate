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

func (r *Svc) Run(t *task.Task) (bool, error) {
	if !r.canDo(t) {
		return false, nil
	}
	defer cleanup(t)
	return true, rsync.RunRsyncJobOverSSH(t, corev1.ServiceTypeClusterIP)
}
