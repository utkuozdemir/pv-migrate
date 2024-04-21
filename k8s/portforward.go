package k8s

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"

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
}

func PortForward(req *PortForwardRequest, logger *slog.Logger) error {
	targetURL, err := url.Parse(req.RestConfig.Host)
	if err != nil {
		return fmt.Errorf("failed to parse target url: %w", err)
	}

	targetURL.Path = path.Join(
		"api", "v1", "namespaces", req.PodNs, "pods", req.PodName, "portforward",
	)

	transport, upgrader, err := spdy.RoundTripperFor(req.RestConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize roundtripper: %w", err)
	}

	outWriter := &slogDebugWriter{logger: logger}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)

	ports := []string{fmt.Sprintf("%d:%d", req.LocalPort, req.PodPort)}

	forwarder, err := portforward.New(dialer, ports, req.StopCh, req.ReadyCh, outWriter, outWriter)
	if err != nil {
		return fmt.Errorf("failed to initialize portforward: %w", err)
	}

	if err = forwarder.ForwardPorts(); err != nil {
		return fmt.Errorf("failed to forward ports: %w", err)
	}

	return nil
}

type slogDebugWriter struct {
	logger *slog.Logger
}

func (w *slogDebugWriter) Write(p []byte) (int, error) {
	w.logger.Debug(string(p))

	return len(p), nil
}
