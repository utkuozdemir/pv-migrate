package strategy

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
	rsyncprogress "github.com/utkuozdemir/pv-migrate/internal/rsync/progress"
)

// stubExitError stands in for the SSH exit error, whose status cannot be set from
// outside its own package.
type stubExitError struct {
	status int
}

func (e *stubExitError) Error() string {
	return fmt.Sprintf("Process exited with status %d", e.status)
}

func (e *stubExitError) ExitStatus() int {
	return e.status
}

// TestCompleteRsyncSessionOnVanishedFiles guards against a hang rather than a
// wrong value. The progress logger retries its stream until it is told the
// transfer finished, so treating exit 24 as a success by returning early would
// leave it running and the group it belongs to would never finish.
func TestCompleteRsyncSessionOnVanishedFiles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slogt.New(t)

	reader, writer := io.Pipe()

	progressLogger := progresslog.NewLogger(progresslog.LoggerOptions{
		Writer:        io.Discard,
		LogStreamFunc: func(context.Context) (io.ReadCloser, error) { return reader, nil },
		ParseLineFunc: rsyncprogress.ParseLine,
	})

	done := make(chan error, 1)

	go func() { done <- progressLogger.Start(ctx, logger) }()

	// The session ended, so its output stream ends with it.
	require.NoError(t, writer.Close())

	var vanished bool

	require.NoError(t, completeRsyncSession(ctx, &stubExitError{status: 24}, progressLogger, &vanished))
	assert.True(t, vanished)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the progress logger never finished, so a vanished-files exit would deadlock the migration")
	}
}

// TestCompleteRsyncSessionOnFailure pins that a real failure comes back marked
// as the session's own, so the caller explains it with the exit status and the
// output tail once the progress goroutines have stopped.
func TestCompleteRsyncSessionOnFailure(t *testing.T) {
	t.Parallel()

	progressLogger := progresslog.NewLogger(progresslog.LoggerOptions{Writer: io.Discard})

	var vanished bool

	err := completeRsyncSession(t.Context(), &stubExitError{status: 23}, progressLogger, &vanished)
	require.Error(t, err)
	assert.False(t, vanished)

	var sessionErr *rsyncRunError

	require.ErrorAs(t, err, &sessionErr)
	assert.Contains(t, sessionErr.Error(), "Process exited with status 23")

	enriched := rsyncSessionError(sessionErr.err, []string{"some raw line"})
	assert.Contains(t, enriched.Error(), `rsync documents this exit code as: Partial transfer due to error`)
}

// TestLineTailCapturesAtTheSource pins the property the tail exists for: it has
// seen everything the moment a write returns, including a final line without a
// newline, so a failure message built right after the session ends is complete.
func TestLineTailCapturesAtTheSource(t *testing.T) {
	t.Parallel()

	tail := &lineTail{limit: 3}

	_, err := tail.Write([]byte("one\ntwo\rthree\nfour\nrsync error: some fatal thing (code 12)"))
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"two", "three", "four", "rsync error: some fatal thing (code 12)"},
		tail.Lines())
}

func TestRsyncSessionErrorKeepsTheRawOutput(t *testing.T) {
	t.Parallel()

	err := rsyncSessionError(&stubExitError{status: 12}, []string{"rsync: connection unexpectedly closed", "tail"})
	require.Error(t, err)

	assert.Contains(t, err.Error(), `rsync documents this exit code as: Error in rsync protocol data stream`)
	assert.Contains(t, err.Error(), "last lines of rsync output:\nrsync: connection unexpectedly closed\ntail")
}

// TestRsyncSessionErrorWithoutAnExitStatus covers the failures that never ran
// rsync at all, where there is no code to interpret.
func TestRsyncSessionErrorWithoutAnExitStatus(t *testing.T) {
	t.Parallel()

	err := rsyncSessionError(io.ErrUnexpectedEOF, nil)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.NotContains(t, err.Error(), "rsync documents")
}
