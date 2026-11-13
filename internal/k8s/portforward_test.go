package k8s_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

func TestPortForwardPreservesAPIServerPathPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hostSuffix string
		wantPath   string
	}{
		{
			name:       "root path",
			hostSuffix: "",
			wantPath:   "/api/v1/namespaces/test-ns/pods/sshd-0/portforward",
		},
		{
			name:       "proxied subpath",
			hostSuffix: "/k8s/clusters/c-1",
			wantPath:   "/k8s/clusters/c-1/api/v1/namespaces/test-ns/pods/sshd-0/portforward",
		},
		{
			name:       "proxied subpath with trailing slash",
			hostSuffix: "/k8s/clusters/c-1/",
			wantPath:   "/k8s/clusters/c-1/api/v1/namespaces/test-ns/pods/sshd-0/portforward",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPath atomic.Value

			srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				gotPath.Store(req.URL.Path)
				rw.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			portCh := make(chan int, 1)
			err := k8s.PortForward(t.Context(), &k8s.PortForwardRequest{
				RestConfig:   &rest.Config{Host: srv.URL + tt.hostSuffix},
				PodNs:        "test-ns",
				PodName:      "sshd-0",
				PodPort:      22,
				ActualPortCh: portCh,
			}, slog.New(slog.DiscardHandler))
			require.Error(t, err)
			assert.Equal(t, tt.wantPath, gotPath.Load())
		})
	}
}
