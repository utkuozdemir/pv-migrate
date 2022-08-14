package log

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestConfigureFancy(t *testing.T) {
	t.Parallel()

	l, err := New(context.Background())
	assert.NoError(t, err)
	err = Configure(l, "debug", "fancy")
	assert.NoError(t, err)
	assert.Equal(t, logrus.DebugLevel, l.Logger.Level)
}

func TestConfigureJson(t *testing.T) {
	t.Parallel()

	l, err := New(context.Background())
	assert.NoError(t, err)
	err = Configure(l, "info", "json")
	assert.NoError(t, err)
	assert.Equal(t, logrus.InfoLevel, l.Logger.Level)
}

func TestBuildInvalidLevel(t *testing.T) {
	t.Parallel()

	l, err := New(context.Background())
	assert.NoError(t, err)
	err = Configure(l, "invalid", "json")
	assert.Error(t, err)
}

func TestBuildInvalidFormat(t *testing.T) {
	t.Parallel()

	l, err := New(context.Background())
	assert.NoError(t, err)
	err = Configure(l, "debug", "invalid")
	assert.Error(t, err)
}
