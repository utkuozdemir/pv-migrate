package log

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureFancy(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	l, err := New(ctx)
	require.NoError(t, err)
	err = Configure(l, "debug", "fancy")
	require.NoError(t, err)
	assert.Equal(t, logrus.DebugLevel, l.Logger.Level)
}

func TestConfigureJson(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	l, err := New(ctx)
	require.NoError(t, err)
	err = Configure(l, "info", "json")
	require.NoError(t, err)
	assert.Equal(t, logrus.InfoLevel, l.Logger.Level)
}

func TestBuildInvalidLevel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	l, err := New(ctx)
	require.NoError(t, err)
	err = Configure(l, "invalid", "json")
	require.Error(t, err)
}

func TestBuildInvalidFormat(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	l, err := New(ctx)
	require.NoError(t, err)
	err = Configure(l, "debug", "invalid")
	require.Error(t, err)
}
