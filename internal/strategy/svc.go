package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

type Svc struct {
}

func (r *Svc) canDo(migrationJob job.Job) bool {
	sameCluster := migrationJob.Source().KubeClient() == migrationJob.Dest().KubeClient()
	return sameCluster
}

func (r *Svc) Run(task task.Task) (bool, error) {
	if !r.canDo(task.Job()) {
		return false, nil
	}
	defer cleanup(task)
	return true, rsync.RunRsyncJobOverSsh(task, corev1.ServiceTypeClusterIP)
}
