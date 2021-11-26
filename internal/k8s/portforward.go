package k8s

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"net/http"
	"net/url"
	"path"
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
	outWriter := logger.WriterLevel(log.InfoLevel)
	defer tryClose(logger, outWriter)

	errWriter := logger.WriterLevel(log.WarnLevel)
	defer tryClose(logger, outWriter)

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)

	ports := []string{fmt.Sprintf("%d:%d", req.LocalPort, req.PodPort)}
	fw, err := portforward.New(dialer, ports, req.StopCh, req.ReadyCh, outWriter, errWriter)
	if err != nil {
		return err
	}

	return fw.ForwardPorts()
}

func tryClose(logger *log.Entry, w io.WriteCloser) {
	err := w.Close()
	if err != nil {
		logger.Debug("failed to close port-forward output stream")
	}
}
