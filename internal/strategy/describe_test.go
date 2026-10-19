package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every strategy the ladder can pick says what it does, in both directions, so
// the step that announces it never comes without its attributes.
func TestEveryStrategyHasADescription(t *testing.T) {
	t.Parallel()

	for name := range nameToStrategy {
		assert.NotEmpty(t, Describe(name, false), name)
		assert.NotEmpty(t, Describe(name, true), name)
	}

	assert.Empty(t, Describe("no-such-strategy", false))
	assert.Equal(t, []any{"sshd", "destination", "rsync", "source", "service", "ClusterIP"},
		Describe(clusterIPStrategy, true))
	assert.Equal(t, []any{"sshd", "source", "rsync", "destination", "service", "ClusterIP"},
		Describe(clusterIPStrategy, false))
}
