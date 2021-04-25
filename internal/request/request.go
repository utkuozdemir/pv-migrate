package request

import log "github.com/sirupsen/logrus"

const (
	DefaultRsyncImage = "docker.io/instrumentisto/rsync-ssh:alpine"
	DefaultSshdImage  = "docker.io/panubo/sshd:1.3.0"
)

type PVC interface {
	KubeconfigPath() string
	Context() string
	Namespace() string
	Name() string
}

type pvc struct {
	kubeconfigPath string
	context        string
	namespace      string
	name           string
}

func (r *pvc) KubeconfigPath() string {
	return r.kubeconfigPath
}

func (r *pvc) Context() string {
	return r.context
}

func (r *pvc) Namespace() string {
	return r.namespace
}

func (r *pvc) Name() string {
	return r.name
}

func NewPVC(kubeconfigPath string, context string, namespace string, name string) PVC {
	return &pvc{
		kubeconfigPath: kubeconfigPath,
		context:        context,
		namespace:      namespace,
		name:           name,
	}
}

type Request interface {
	Source() PVC
	Dest() PVC
	Options() Options
	Strategies() []string
	LogFields() log.Fields
	RsyncImage() string
	SshdImage() string
}

type request struct {
	source     PVC
	dest       PVC
	options    Options
	strategies []string
	rsyncImage string
	sshdImage  string
}

func (r *request) Source() PVC {
	return r.source
}

func (r *request) Dest() PVC {
	return r.dest
}

func (r *request) Options() Options {
	return r.options
}

func (r *request) Strategies() []string {
	return r.strategies
}

func (r *request) RsyncImage() string {
	return r.rsyncImage
}

func (r *request) SshdImage() string {
	return r.sshdImage
}

func (r *request) LogFields() log.Fields {
	return log.Fields{
		"source": r.Source().Namespace() + "/" + r.Source().Name(),
		"dest":   r.Dest().Name() + "/" + r.Dest().Name(),
	}
}

func NewWithDefaultImages(source PVC, dest PVC, options Options, strategies []string) Request {
	return New(source, dest, options, strategies, DefaultRsyncImage, DefaultSshdImage)
}

func New(source PVC, dest PVC, options Options, strategies []string, rsyncImage string, sshdImage string) Request {
	return &request{
		source:     source,
		dest:       dest,
		options:    options,
		strategies: strategies,
		rsyncImage: rsyncImage,
		sshdImage:  sshdImage,
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

func (r *options) DeleteExtraneousFiles() bool {
	return r.deleteExtraneousFiles
}
