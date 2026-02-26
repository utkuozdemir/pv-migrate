package progress_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

func TestParseLogLineEndMatch(t *testing.T) {
	t.Parallel()

	l := "total size is 1,879,048,192  speedup is 31,548.30"
	p, err := progress.ParseLine(l)
	require.NoError(t, err)
	assert.Equal(t, 100, p.Percentage)
	assert.Equal(t, int64(1879048192), p.Transferred)
	assert.Equal(t, int64(1879048192), p.Total)
}
