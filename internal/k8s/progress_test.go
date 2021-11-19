package k8s

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseLogLineEndMatch(t *testing.T) {
	l := "total size is 1,879,048,192  speedup is 31,548.30"
	p, err := parseLogLine(&l)
	assert.NoError(t, err)
	assert.Equal(t, 100, p.percentage)
	assert.Equal(t, int64(1879048192), p.transferred)
	assert.Equal(t, int64(1879048192), p.total)
}
