package k8s_test

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

func TestHelmRESTClientGetterDoesNotMutateRestConfig(t *testing.T) {
	t.Parallel()

	cfg := &rest.Config{
		Host:  "https://127.0.0.1:6443",
		Burst: 15,
	}

	getter := k8s.NewRESTClientGetter(cfg, nil, slog.New(slog.DiscardHandler))

	disc, err := getter.ToDiscoveryClient()
	require.NoError(t, err)
	require.NotNil(t, disc)
	assert.Equal(t, 15, cfg.Burst)
}

func TestHelmRESTClientGetterPropagatesInvalidConfigError(t *testing.T) {
	t.Parallel()

	cfg := &rest.Config{
		Host:      "https://127.0.0.1:6443",
		Transport: http.DefaultTransport,
		Insecure:  true,
	}

	getter := k8s.NewRESTClientGetter(cfg, nil, slog.New(slog.DiscardHandler))

	disc, err := getter.ToDiscoveryClient()
	require.Error(t, err)
	assert.Nil(t, disc)
}
