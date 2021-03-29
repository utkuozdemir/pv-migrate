package migration

import log "github.com/sirupsen/logrus"

type RequestPvc interface {
	KubeconfigPath() string
	Context() string
	Namespace() string
	Name() string
}

type requestPvc struct {
	kubeconfigPath string
	context        string
	namespace      string
	name           string
}

func (r *requestPvc) KubeconfigPath() string {
	return r.kubeconfigPath
}

func (r *requestPvc) Context() string {
	return r.context
}

func (r *requestPvc) Namespace() string {
	return r.namespace
}

func (r *requestPvc) Name() string {
	return r.name
}

func NewRequestPvc(kubeconfigPath string, context string, namespace string, name string) RequestPvc {
	return &requestPvc{
		kubeconfigPath: kubeconfigPath,
		context:        context,
		namespace:      namespace,
		name:           name,
	}
}

type Request interface {
	Source() RequestPvc
	Dest() RequestPvc
	Options() RequestOptions
	Strategies() []string
	LogFields() log.Fields
}

type request struct {
	source     RequestPvc
	dest       RequestPvc
	options    RequestOptions
	strategies []string
}

func (r *request) Source() RequestPvc {
	return r.source
}

func (r *request) Dest() RequestPvc {
	return r.dest
}

func (r *request) Options() RequestOptions {
	return r.options
}

func (r *request) Strategies() []string {
	return r.strategies
}

func (r *request) LogFields() log.Fields {
	return log.Fields{
		"source": r.Source().Namespace() + "/" + r.Source().Name(),
		"dest":   r.Dest().Name() + "/" + r.Dest().Name(),
	}
}

func NewRequest(source RequestPvc, dest RequestPvc, options RequestOptions, strategies []string) Request {
	return &request{
		source:     source,
		dest:       dest,
		options:    options,
		strategies: strategies,
	}
}

type RequestOptions interface {
	DeleteExtraneousFiles() bool
}

type requestOptions struct {
	deleteExtraneousFiles bool
}

func NewRequestOptions(deleteExtraneousFiles bool) RequestOptions {
	return &requestOptions{deleteExtraneousFiles: deleteExtraneousFiles}
}

func (r *requestOptions) DeleteExtraneousFiles() bool {
	return r.deleteExtraneousFiles
}
