package migration

import log "github.com/sirupsen/logrus"

type Request struct {
	SourceKubeconfigPath string
	SourceContext        string
	SourceNamespace      string
	SourceName           string
	DestKubeconfigPath   string
	DestContext          string
	DestNamespace        string
	DestName             string
	Options              RequestOptions
	Strategies           []string
}

type RequestOptions struct {
	DeleteExtraneousFiles bool
}

func (request *Request) LogFields() log.Fields {
	return log.Fields{
		"source": request.SourceNamespace + "/" + request.SourceName,
		"dest":   request.DestNamespace + "/" + request.DestName,
	}
}
