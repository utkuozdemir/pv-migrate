package strategy_test

import (
	"bytes"
	"context"
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

	wrote := make(chan struct{}, 1)
	barOut := &notifyingWriter{
		dest:  &out,
		wrote: wrote,
	}

	progressLogger := strategy.SessionProgressLogger(&migration.Request{
		Writer:          barOut,
		ShowProgressBar: true,
		StructuredLogs:  false,
	}, reader)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- progressLogger.Start(ctx, slog.New(slog.DiscardHandler))
	}()

	_, err := writer.Write([]byte(progressLine))
	require.NoError(t, err)

	<-wrote
	cancel()
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	require.True(t, progressLogger.Rendered())
	require.False(t, strings.HasSuffix(out.String(), "\n"))

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

type notifyingWriter struct {
	dest  io.Writer
	wrote chan<- struct{}
}

func (w *notifyingWriter) Write(data []byte) (int, error) {
	written, err := w.dest.Write(data)

	select {
	case w.wrote <- struct{}{}:
	default:
	}

	return written, err //nolint:wrapcheck
}
