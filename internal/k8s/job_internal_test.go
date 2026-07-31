package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFollowLogOptions pins the retry contract of the follow stream: a retried
// follow must not replay the log from the start, because the replay re-drives
// the progress bar to 100% once per retry and strands a completed bar line
// each time.
func TestFollowLogOptions(t *testing.T) {
	t.Parallel()

	first := followLogOptions(false)
	assert.True(t, first.Follow)
	assert.Nil(t, first.TailLines, "the first open reads the whole log")

	retry := followLogOptions(true)
	assert.True(t, retry.Follow)
	require.NotNil(t, retry.TailLines, "a retry must resume at the tail")
	assert.Zero(t, *retry.TailLines)
}
