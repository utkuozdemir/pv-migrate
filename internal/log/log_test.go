package log

import (
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBuildLoggerFancy(t *testing.T) {
	l, err := BuildLogger(logrus.New(), "debug", "fancy")
	assert.NoError(t, err)
	assert.Equal(t, logrus.DebugLevel, l.Logger.Level)
}

func TestBuildLoggerJson(t *testing.T) {
	l, err := BuildLogger(logrus.New(), "info", "json")
	assert.NoError(t, err)
	assert.Equal(t, logrus.InfoLevel, l.Logger.Level)
}

func TestBuildInvalidLevel(t *testing.T) {
	_, err := BuildLogger(logrus.New(), "invalid", "json")
	assert.Error(t, err)
}

func TestBuildInvalidFormat(t *testing.T) {
	_, err := BuildLogger(logrus.New(), "debug", "invalid")
	assert.Error(t, err)
}
