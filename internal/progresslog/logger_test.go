package progresslog_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/progresslog"
)

func TestLoggerRetriesAfterStreamEOF(t *testing.T) {
	t.Parallel()

	var streamCalls atomic.Int32

	var parsedLines atomic.Int32

	logger := progresslog.NewLogger(progresslog.LoggerOptions{
		Writer: io.Discard,
		LogStreamFunc: func(context.Context) (io.ReadCloser, error) {
			if streamCalls.Add(1) == 1 {
				return io.NopCloser(strings.NewReader("")), nil
			}

			return io.NopCloser(strings.NewReader("progress\n")), nil
		},
		ParseLineFunc: func(string) (progresslog.Update, error) {
			parsedLines.Add(1)

			return progresslog.Update{Transferred: 1, Total: 1, Percentage: 100}, nil
		},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- logger.Start(ctx, slog.New(slog.DiscardHandler))
	}()

	require.Eventually(t, func() bool {
		return streamCalls.Load() >= 2 && parsedLines.Load() >= 1
	}, time.Second, 25*time.Millisecond)

	require.NoError(t, logger.MarkAsComplete(ctx))
	require.NoError(t, <-done)
}
