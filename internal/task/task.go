package task

import (
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

type Task interface {
	ID() string
	Job() job.Job
}

type task struct {
	id  string
	job job.Job
}

func (t *task) ID() string {
	return t.id
}

func (t *task) Job() job.Job {
	return t.job
}

func New(job job.Job) Task {
	return &task{
		id:  util.RandomHexadecimalString(5),
		job: job,
	}
}
