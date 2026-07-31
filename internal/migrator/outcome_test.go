package migrator

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

func failingMigrator(errs map[string]error) Migrator {
	strategies := make(map[string]strategy.Strategy, len(errs))

	for name, err := range errs {
		strategies[name] = &mockStrategy{
			runFunc: func(context.Context, *migration.Attempt) error { return err },
		}
	}

	return Migrator{
		getKubeClient: fakeClusterClientGetter(),
		getStrategyMap: func([]string) (map[string]strategy.Strategy, error) {
			return strategies, nil
		},
	}
}

func TestRunSummarizesEveryOutcome(t *testing.T) {
	t.Parallel()

	failure := errors.New("failed to get service address: timed out after 2m0s")

	mig := failingMigrator(map[string]error{
		"mount":        strategy.Declined("source and destination are in different namespaces"),
		"loadbalancer": failure,
		"bare":         strategy.ErrUnaccepted,
	})

	var out bytes.Buffer

	req := buildMigrationRequestWithStrategies([]string{"mount", "loadbalancer", "bare"}, true)
	req.Writer = &out

	err := mig.Run(t.Context(), req, slogt.New(t))
	require.Error(t, err)

	assert.Equal(t, "no strategy could complete the migration", err.Error(),
		"the message is rendered as a single log attribute, so it stays one line")
	require.ErrorIs(t, err, failure, "the per-strategy errors stay reachable through the unwrap tree")
	require.ErrorIs(t, err, strategy.ErrUnaccepted)

	summary := out.String()

	assert.Contains(t, summary, "Migration failed: no strategy could complete the migration.")
	assert.Contains(t, summary, "mount")
	assert.Contains(t, summary, "declined")
	assert.Contains(t, summary, "source and destination are in different namespaces")
	assert.Contains(t, summary, "failed")
	assert.Contains(t, summary, "failed to get service address: timed out after 2m0s")
	assert.Contains(t, summary, "unaccepted",
		"a decline with no reason must render its own text rather than a blank row")
	assert.NotContains(t, summary, "clusterip", "only the requested strategies are listed")
}

func TestRunSummaryIndentsMultiLineFailures(t *testing.T) {
	t.Parallel()

	mig := failingMigrator(map[string]error{
		"mount": errors.New("first line\nsecond line"),
	})

	var out bytes.Buffer

	req := buildMigrationRequestWithStrategies([]string{"mount"}, true)
	req.Writer = &out

	require.Error(t, mig.Run(t.Context(), req, slogt.New(t)))
	assert.Contains(t, out.String(), "Migration failed: the mount strategy failed.\n",
		"a single-strategy run reads as a sentence, not a one-row table")
	assert.Contains(t, out.String(), "\n    first line\n    second line\n",
		"the failure text sits on its own indented lines, whole")
}

// TestRunSummaryColorsWhenRequested pins that the color plumbing reaches the
// summary, and that it is off by default so every plain-text assertion above
// stays exact.
func TestRunSummaryColorsWhenRequested(t *testing.T) {
	t.Parallel()

	mig := failingMigrator(map[string]error{"mount": errors.New("boom")})

	var out bytes.Buffer

	req := buildMigrationRequestWithStrategies([]string{"mount"}, true)
	req.Writer = &out
	req.ColorOutput = true

	require.Error(t, mig.Run(t.Context(), req, slogt.New(t)))
	assert.Contains(t, out.String(), "\x1b[1;31mMigration failed: the mount strategy failed.\x1b[0m")
}

func TestRunWithoutWriterDoesNotPanic(t *testing.T) {
	t.Parallel()

	mig := failingMigrator(map[string]error{"mount": errors.New("boom")})

	req := buildMigrationRequestWithStrategies([]string{"mount"}, true)
	req.Writer = nil

	require.Error(t, mig.Run(t.Context(), req, slogt.New(t)))
}

func TestRunPrintsNothingWhenAStrategySucceeds(t *testing.T) {
	t.Parallel()

	mig := failingMigrator(map[string]error{
		"mount": strategy.Declined("source and destination are in different namespaces"),
		"ok":    nil,
	})

	var out bytes.Buffer

	req := buildMigrationRequestWithStrategies([]string{"mount", "ok"}, true)
	req.Writer = &out

	require.NoError(t, mig.Run(t.Context(), req, slogt.New(t)))
	assert.Empty(t, out.String())
}

func TestRunSuppressesTheSummaryForStructuredLogs(t *testing.T) {
	t.Parallel()

	mig := failingMigrator(map[string]error{"mount": errors.New("boom")})

	var out bytes.Buffer

	req := buildMigrationRequestWithStrategies([]string{"mount"}, true)
	req.Writer = &out
	req.StructuredLogs = true

	require.Error(t, mig.Run(t.Context(), req, slogt.New(t)))
	assert.Empty(t, out.String(), "a plain-text block would corrupt the JSON records on the same stream")
}
