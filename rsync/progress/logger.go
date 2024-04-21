package progress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
)

type LogStreamFunc func(ctx context.Context) (io.ReadCloser, error)

type Logger struct {
	options   LoggerOptions
	successCh chan struct{}
}

type LoggerOptions struct {
	ShowProgressBar bool
	LogStreamFunc   LogStreamFunc
}

func NewLogger(options LoggerOptions) *Logger {
	return &Logger{
		options:   options,
		successCh: make(chan struct{}, 1),
	}
}

func (l *Logger) Start(ctx context.Context, logger *slog.Logger) error {
	for {
		err := l.startSingle(ctx, logger)
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}

		logger.Debug("log tail failed, retrying", "error", err)
	}
}

func (l *Logger) MarkAsComplete(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	case l.successCh <- struct{}{}:
	}

	return nil
}

func (l *Logger) startSingle(ctx context.Context, logger *slog.Logger) error {
	logCh := make(chan string)

	var eg errgroup.Group //nolint:varnamelen

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logStream, err := l.options.LogStreamFunc(ctx)
	if err != nil {
		return fmt.Errorf("failed to get log stream: %w", err)
	}

	defer func() {
		if closeErr := logStream.Close(); closeErr != nil {
			logger.Error("failed to close log stream", "error", closeErr)
		}
	}()

	eg.Go(func() error {
		defer cancel()

		return tailLogs(ctx, logStream, logCh)
	})

	eg.Go(func() error {
		defer cancel()

		return handleLogs(ctx, logCh, l.successCh, l.options.ShowProgressBar, logger)
	})

	if err = eg.Wait(); err != nil {
		return fmt.Errorf("failed to wait for log tailing: %w", err)
	}

	return nil
}

func tailLogs(ctx context.Context, stream io.Reader, logCh chan<- string) error {
	scanner := bufio.NewScanner(stream)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		default:
			if scanner.Scan() {
				select {
				case <-ctx.Done():
					return ctx.Err() //nolint:wrapcheck
				case logCh <- scanner.Text():
				}
			}
		}
	}
}

//nolint:cyclop
func handleLogs(ctx context.Context, logCh <-chan string, successCh <-chan struct{},
	showProgressBar bool, logger *slog.Logger,
) error {
	var progressBar *progressbar.ProgressBar

	if showProgressBar {
		progressBar = progressbar.NewOptions64(
			1,
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionFullWidth(),
			progressbar.OptionOnCompletion(func() {
				_, _ = fmt.Fprintln(os.Stderr)
			}),
			progressbar.OptionSetDescription("📂 Copying data..."),
		)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case <-successCh:
			if showProgressBar {
				if err := progressBar.Finish(); err != nil {
					logger.Debug("failed to finish progress bar", "error", err)
				}
			}

			return nil
		case logLine := <-logCh:
			progress, err := ParseLine(logLine)
			if err != nil {
				logger.Log(ctx, slog.LevelDebug-1, "failed to parse progress line", "error", err)

				continue
			}

			if !showProgressBar {
				logger.Debug(logLine, slog.String("source", "rsync"), slog.Group("progress", "transferred",
					progress.Transferred, "total", progress.Total, "percentage", progress.Percentage))
			} else {
				if err = updateProgressBar(progressBar, progress.Transferred, progress.Total); err != nil {
					logger.Warn("failed to update progress bar", "error", err, "progress", progress)
				}
			}

			if progress.Percentage >= 100 {
				return nil
			}
		}
	}
}

func updateProgressBar(progressBar *progressbar.ProgressBar, transferred, total int64) error {
	progressBar.ChangeMax64(total)

	if total == 0 { // cannot update progress bar when its max is 0
		return nil
	}

	if err := progressBar.Set64(transferred); err != nil {
		return fmt.Errorf("failed to set progress bar value: %w", err)
	}

	return nil
}
