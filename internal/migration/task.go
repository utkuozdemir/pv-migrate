package migration

import "github.com/utkuozdemir/pv-migrate/internal/k8s"

type Task interface {
	Id() string
	Source() k8s.PvcInfo
	Dest() k8s.PvcInfo
	Options() TaskOptions
}

type task struct {
	id      string
	source  k8s.PvcInfo
	dest    k8s.PvcInfo
	options TaskOptions
}

func (t *task) Id() string {
	return t.id
}

func (t *task) Source() k8s.PvcInfo {
	return t.source
}

func (t *task) Dest() k8s.PvcInfo {
	return t.dest
}

func (t *task) Options() TaskOptions {
	return t.options
}

func NewTask(id string, source k8s.PvcInfo, dest k8s.PvcInfo, options TaskOptions) Task {
	return &task{
		id:      id,
		source:  source,
		dest:    dest,
		options: options,
	}
}

type TaskOptions interface {
	DeleteExtraneousFiles() bool
}

type taskOptions struct {
	deleteExtraneousFiles bool
}

func NewTaskOptions(deleteExtraneousFiles bool) TaskOptions {
	return &taskOptions{deleteExtraneousFiles: deleteExtraneousFiles}
}

func (t *taskOptions) DeleteExtraneousFiles() bool {
	return t.deleteExtraneousFiles
}
