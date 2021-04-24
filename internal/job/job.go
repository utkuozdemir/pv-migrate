package job

import "github.com/utkuozdemir/pv-migrate/internal/pvc"

type Job interface {
	Source() pvc.Info
	Dest() pvc.Info
	Options() Options
}

type Options interface {
	DeleteExtraneousFiles() bool
}

type job struct {
	source  pvc.Info
	dest    pvc.Info
	options Options
}

func (t *job) Source() pvc.Info {
	return t.source
}

func (t *job) Dest() pvc.Info {
	return t.dest
}

func (t *job) Options() Options {
	return t.options
}

func New(id string, source pvc.Info, dest pvc.Info, options Options) Job {
	return &job{
		source:  source,
		dest:    dest,
		options: options,
	}
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
