package k8s

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PortForwardRequest struct {
	// RestConfig is the kubernetes config
	RestConfig *rest.Config
	PodNs      string
	PodName    string
	LocalPort  int
	PodPort    int
	StopCh     <-chan struct{}
	ReadyCh    chan struct{}
	Logger     *log.Entry
}

func PortForward(req *PortForwardRequest) error {
	targetURL, err := url.Parse(req.RestConfig.Host)
	if err != nil {
		return err
	}

	targetURL.Path = path.Join(
		"api", "v1", "namespaces", req.PodNs, "pods", req.PodName, "portforward",
	)

	transport, upgrader, err := spdy.RoundTripperFor(req.RestConfig)
	if err != nil {
		return err
	}

	logger := req.Logger

	outWriter := logger.WriterLevel(log.DebugLevel)
	defer tryClose(logger, outWriter)

	errWriter := logger.WriterLevel(log.DebugLevel)
	defer tryClose(logger, errWriter)

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)

	ports := []string{fmt.Sprintf("%d:%d", req.LocalPort, req.PodPort)}

	forwarder, err := portforward.New(dialer, ports, req.StopCh, req.ReadyCh, outWriter, errWriter)
	if err != nil {
		return err
	}

	return forwarder.ForwardPorts()
}

func tryClose(logger *log.Entry, w io.WriteCloser) {
	if err := w.Close(); err != nil {
		logger.Debug("failed to close port-forward output stream")
	}
}
