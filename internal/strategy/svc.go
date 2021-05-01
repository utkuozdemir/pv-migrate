package strategy

import (
	"errors"
	"github.com/hashicorp/go-multierror"
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	corev1 "k8s.io/api/core/v1"
)

const (
	SvcName = "svc"
)

type Svc struct {
}

func (r *Svc) Cleanup(task task.Task) error {
	migrationJob := task.Job()
	var result *multierror.Error
	err := k8s.CleanupForID(migrationJob.Source().KubeClient(), migrationJob.Source().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = k8s.CleanupForID(migrationJob.Dest().KubeClient(), migrationJob.Dest().Claim().Namespace, task.ID())
	if err != nil {
		result = multierror.Append(result, err)
	}
	//goland:noinspection GoNilness
	return result.ErrorOrNil()
}

func (r *Svc) Name() string {
	return SvcName
}

func (r *Svc) CanDo(migrationJob job.Job) bool {
	sameCluster := migrationJob.Source().KubeClient() == migrationJob.Dest().KubeClient()
	return sameCluster
}

func (r *Svc) Run(task task.Task) error {
	if !r.CanDo(task.Job()) {
		return errors.New("cannot do this task using this strategy")
	}
	return rsync.RunRsyncJobOverSsh(task, corev1.ServiceTypeClusterIP)
}
