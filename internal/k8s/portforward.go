package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"

	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PortForwardRequest struct {
	// RestConfig is the kubernetes config
	RestConfig *rest.Config
	PodNs      string
	PodName    string
	PodPort    int
	// ActualPortCh receives the OS-assigned local port once the port-forward is ready.
	// Must be a buffered channel of size ≥ 1 so the send is non-blocking.
	ActualPortCh chan<- int
}

func PortForward(ctx context.Context, req *PortForwardRequest, logger *slog.Logger) error {
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

	dialer := spdy.NewDialer(
		upgrader,
		&http.Client{Transport: transport},
		http.MethodPost,
		targetURL,
	)

	// Port "0:PodPort" tells the library to let the OS pick the local port,
	// eliminating the probe-close-reuse TOCTOU race of the old getFreePort approach.
	ports := []string{fmt.Sprintf("0:%d", req.PodPort)}
	internalReadyCh := make(chan struct{})

	eg, ctx := errgroup.WithContext(ctx)

	forwarder, err := portforward.New(dialer, ports, ctx.Done(), internalReadyCh, outWriter, outWriter)
	if err != nil {
		return fmt.Errorf("failed to initialize portforward: %w", err)
	}

	// Port notifier: waits for the forwarder to bind the port, then sends the
	// actual OS-assigned local port on ActualPortCh so the caller can use it.
	eg.Go(func() error {
		select {
		case <-ctx.Done():
			return nil
		case <-internalReadyCh:
		}

		fwdPorts, fwdErr := forwarder.GetPorts()
		if fwdErr != nil {
			return fmt.Errorf("failed to get forwarded ports: %w", fwdErr)
		}

		req.ActualPortCh <- int(fwdPorts[0].Local)

		return nil
	})

	eg.Go(func() error {
		if fwdErr := forwarder.ForwardPorts(); fwdErr != nil {
			return fmt.Errorf("failed to forward ports: %w", fwdErr)
		}

		return nil
	})

	return eg.Wait() //nolint:wrapcheck
}

type slogDebugWriter struct {
	logger *slog.Logger
}

func (w *slogDebugWriter) Write(p []byte) (int, error) {
	w.logger.Debug(string(p))

	return len(p), nil
}
