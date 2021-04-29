package job

import "github.com/utkuozdemir/pv-migrate/internal/pvc"

type Job interface {
	Source() pvc.Info
	Dest() pvc.Info
	Options() Options
	RsyncImage() string
	SshdImage() string
}

type Options interface {
	DeleteExtraneousFiles() bool
	NoChown() bool
}

type job struct {
	source     pvc.Info
	dest       pvc.Info
	options    Options
	rsyncImage string
	sshdImage  string
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

func (t *job) RsyncImage() string {
	return t.rsyncImage
}

func (t *job) SshdImage() string {
	return t.sshdImage
}

func New(source pvc.Info, dest pvc.Info, options Options, rsyncImage string, sshdImage string) Job {
	return &job{
		source:     source,
		dest:       dest,
		options:    options,
		rsyncImage: rsyncImage,
		sshdImage:  sshdImage,
	}
}

type options struct {
	deleteExtraneousFiles bool
	noChown               bool
}

func NewOptions(deleteExtraneousFiles bool, noChown bool) Options {
	return &options{deleteExtraneousFiles: deleteExtraneousFiles, noChown: noChown}
}

func (t *options) DeleteExtraneousFiles() bool {
	return t.deleteExtraneousFiles
}

func (t *options) NoChown() bool {
	return t.noChown
}
