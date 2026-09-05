package strategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every strategy the ladder can pick has a line that says what it does, in
// both directions, so the step that announces it never dangles.
func TestEveryStrategyHasADescription(t *testing.T) {
	t.Parallel()

	for name := range nameToStrategy {
		assert.NotEmpty(t, Describe(name, false), name)
		assert.NotEmpty(t, Describe(name, true), name)
	}

	assert.Empty(t, Describe("no-such-strategy", false))
	assert.Contains(t, Describe(clusterIPStrategy, true), "sshd next to the destination")
	assert.Contains(t, Describe(clusterIPStrategy, false), "sshd next to the source")
}
