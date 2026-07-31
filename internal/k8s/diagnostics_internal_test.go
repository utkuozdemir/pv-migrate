package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetachedContextSurvivesParentCancellation pins the property the fake
// clientset cannot: diagnostics run on the failure path, where the parent
// context may already be over, and that must not mean no diagnostics at all.
func TestDetachedContextSurvivesParentCancellation(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(t.Context())
	cancel()

	detached, detachedCancel := detachedContext(parent)
	defer detachedCancel()

	require.NoError(t, detached.Err(), "a cancelled parent must not cancel the diagnostics context")

	deadline, ok := detached.Deadline()
	assert.True(t, ok, "the diagnostics context has to bound its own work instead")
	assert.False(t, deadline.IsZero())
}
