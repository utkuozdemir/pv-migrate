package rsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogLineEndMatch(t *testing.T) {
	t.Parallel()

	l := "total size is 1,879,048,192  speedup is 31,548.30"
	p, err := parseLine(&l)
	require.NoError(t, err)
	assert.Equal(t, 100, p.percentage)
	assert.Equal(t, int64(1879048192), p.transferred)
	assert.Equal(t, int64(1879048192), p.total)
}
