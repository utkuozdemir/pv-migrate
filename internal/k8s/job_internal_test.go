package k8s

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
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

func TestWarnCompletionMarkers(t *testing.T) {
	t.Parallel()

	var withoutMarker bytes.Buffer

	warnIfSourceFilesVanished("regular pod log output\n", slog.New(slog.NewTextHandler(&withoutMarker, nil)))
	warnIfPosixMetadataMissing("regular pod log output\n", slog.New(slog.NewTextHandler(&withoutMarker, nil)))
	assert.Empty(t, withoutMarker.String())

	var withMarkers bytes.Buffer

	tail := "line 1\n" + rsync.VanishedFilesMarker + "\n" + rclone.MissingPosixMetadataMarker + "\nline 4\n"
	logger := slog.New(slog.NewTextHandler(&withMarkers, nil))
	warnIfSourceFilesVanished(tail, logger)
	warnIfPosixMetadataMissing(tail, logger)
	assert.Contains(t, withMarkers.String(), "level=WARN")
	assert.Contains(
		t,
		withMarkers.String(),
		"🔶 Some source files vanished during the transfer and were skipped, re-run to copy them",
	)
	assert.Contains(
		t,
		withMarkers.String(),
		"🔶 Backup has no POSIX metadata, file permissions and ownership were not restored",
	)
}
