package migrator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lmittmann/tint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

// TestRunNarratesEachAttemptUnderItsStep pins the shape of a run's text output:
// the opening step with its facts, then one step per attempt with its own
// outcome under it, a decline, a failure whose multi-line error stays on its
// line, and a success that closes the run.
func TestRunNarratesEachAttemptUnderItsStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := slog.New(narrate.NewHandler(tint.NewTextHandler(&buf, &tint.Options{
		NoColor: true,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}

			return attr
		},
	})))

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

	assertLinesInOrder(
		t,
		buf.String(),
		"INF 🚀 Migrating source=namespace1/pvc1 dest=namespace2/pvc2 id=",
		"INF    📌 Source claim pvc=namespace1/pvc1 size=512Mi access_modes=ReadOnlyMany mounted_on=node1",
		"INF    📌 Destination claim pvc=namespace2/pvc2 size=512Mi access_modes=ReadWriteOnce,ReadWriteMany "+
			"mounted_on=node2",
		"INF    🏠 ",
		"INF    💡 Claim is mounted, continuing because --ignore-mounted is set pvc=namespace1/pvc1 mounted_on=node1",
		"INF    🧭 Strategies to try strategies=declines,fails,works",
		"INF 🚁 Trying strategy strategy=declines",
		`INF    🦊 Strategy does not apply reason="the claims are in different namespaces"`,
		"INF 🚁 Trying strategy strategy=fails",
		`WRN    🔶 Strategy failed error="the data mover exited with code 12\nlast lines of rsync output:\nconnection closed"`,
		"INF 🚁 Trying strategy strategy=works",
		"INF ✅ Migration succeeded strategy=works duration=",
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
