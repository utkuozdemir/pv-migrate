package task

import (
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

type Task interface {
	Id() string
	Source() pvc.Info
	Dest() pvc.Info
	Options() Options
}

type task struct {
	id      string
	source  pvc.Info
	dest    pvc.Info
	options Options
}

func (t *task) Id() string {
	return t.id
}

func (t *task) Source() pvc.Info {
	return t.source
}

func (t *task) Dest() pvc.Info {
	return t.dest
}

func (t *task) Options() Options {
	return t.options
}

func New(id string, source pvc.Info, dest pvc.Info, options Options) Task {
	return &task{
		id:      id,
		source:  source,
		dest:    dest,
		options: options,
	}
}

type Options interface {
	DeleteExtraneousFiles() bool
}

type options struct {
	deleteExtraneousFiles bool
}

func NewOptions(deleteExtraneousFiles bool) Options {
	return &options{deleteExtraneousFiles: deleteExtraneousFiles}
}

func (t *options) DeleteExtraneousFiles() bool {
	return t.deleteExtraneousFiles
}
