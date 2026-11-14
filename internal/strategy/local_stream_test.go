package strategy_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

func TestSessionProgressLogger_StructuredLogsSuppressesBar(t *testing.T) {
	t.Parallel()

	const progressLine = "      1,048,576  50%    1.00MB/s    0:00:01\n"

	reader, writer := io.Pipe()

	var structuredOut bytes.Buffer

	structuredLogger := strategy.SessionProgressLogger(&migration.Request{
		Writer:          &structuredOut,
		ShowProgressBar: true,
		StructuredLogs:  true,
	}, reader)

	done := make(chan error, 1)

	go func() {
		done <- structuredLogger.Start(t.Context(), slog.New(slog.DiscardHandler))
	}()

	_, err := writer.Write([]byte(progressLine))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, structuredLogger.MarkAsComplete(t.Context()))
	require.NoError(t, <-done)

	assert.Empty(t, structuredOut.String())
	assert.False(t, structuredLogger.Rendered())
}

func TestFinishRsyncSession_TerminatesRenderedBarOnError(t *testing.T) {
	t.Parallel()

	const progressLine = "      1,048,576  50%    1.00MB/s    0:00:01\n"

	reader, writer := io.Pipe()

	var out bytes.Buffer

	progressLogger := strategy.SessionProgressLogger(&migration.Request{
		Writer:          &out,
		ShowProgressBar: true,
		StructuredLogs:  false,
	}, reader)

	done := make(chan error, 1)

	go func() {
		done <- progressLogger.Start(t.Context(), slog.New(slog.DiscardHandler))
	}()

	_, err := writer.Write([]byte(progressLine))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, progressLogger.MarkAsComplete(t.Context()))
	require.NoError(t, <-done)
	require.True(t, progressLogger.Rendered())

	sessionErr := strategy.FinishRsyncSession(
		errors.New("ssh tunnel failed"),
		false,
		strategy.NewLineTail(5),
		progressLogger,
		&out,
		slog.New(slog.DiscardHandler),
	)
	require.ErrorContains(t, sessionErr, "ssh tunnel failed")
	assert.True(t, strings.HasSuffix(out.String(), "\n"))
}
