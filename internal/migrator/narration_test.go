package migrator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

// TestRunNarratesEachAttemptUnderItsStep pins the shape of a run's text output:
// the opening step with its facts, then one step per attempt with its own
// outcome under it, a decline, a failure with a line break in its error, and a
// success that closes the run.
func TestRunNarratesEachAttemptUnderItsStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := slog.New(narrate.NewHandler(&buf, narrate.Options{}))

	declines := &mockStrategy{runFunc: func(context.Context, *migration.Attempt) error {
		return strategy.Declined("the claims are in different namespaces")
	}}
	fails := &mockStrategy{runFunc: func(context.Context, *migration.Attempt) error {
		return errors.New("the data mover exited with code 12\nlast lines of rsync output:\nconnection closed")
	}}
	works := &mockStrategy{runFunc: func(context.Context, *migration.Attempt) error { return nil }}

	migrator := Migrator{
		getKubeClient: fakeClusterClientGetter(),
		getStrategyMap: func([]string) (map[string]strategy.Strategy, error) {
			return map[string]strategy.Strategy{"declines": declines, "fails": fails, "works": works}, nil
		},
	}

	req := buildMigrationRequestWithStrategies([]string{"declines", "fails", "works"}, true)
	require.NoError(t, migrator.Run(t.Context(), req, logger))

	assertLinesInOrder(t, buf.String(),
		"🚀 Migrating namespace1/pvc1 to namespace2/pvc2",
		"   🆔 migration id ",
		"   📌 source namespace1/pvc1, 512Mi, ReadOnlyMany, mounted on node node1",
		"   📌 destination namespace2/pvc2, 512Mi, ReadWriteOnce+ReadWriteMany, mounted on node node2",
		"   🏠 ",
		"   💡 namespace1/pvc1 is mounted on node node1, continuing because --ignore-mounted is set",
		"   🧭 trying declines, fails, works, in that order",
		"🚁 declines",
		"   🦊 does not apply: the claims are in different namespaces",
		"🚁 fails",
		"   🔶 failed: the data mover exited with code 12",
		"      last lines of rsync output:",
		"      connection closed",
		"🚁 works",
		"✅ Migration succeeded over works in ",
	)
}

// assertLinesInOrder checks that the output has a line starting with each
// prefix, in this order, with any lines in between.
func assertLinesInOrder(t *testing.T, output string, prefixes ...string) {
	t.Helper()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	next := 0

	for _, line := range lines {
		if next < len(prefixes) && strings.HasPrefix(line, prefixes[next]) {
			next++
		}
	}

	assert.Len(t, prefixes, next, "missing in order: %q\n\noutput:\n%s", prefixes[min(next, len(prefixes)-1)], output)
}
