package progresslog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
)

const (
	logRetryInitialDelay = 250 * time.Millisecond
	logRetryMaxDelay     = 5 * time.Second
)

type LogStreamFunc func(ctx context.Context) (io.ReadCloser, error)

type ParseLineFunc func(string) (Update, error)

type Update struct {
	Line        string
	Percentage  int
	Transferred int64
	Total       int64
}

type Logger struct {
	options   LoggerOptions
	successCh chan struct{}
}

type LoggerOptions struct {
	Writer          io.Writer
	ShowProgressBar bool
	LogStreamFunc   LogStreamFunc
	ParseLineFunc   ParseLineFunc
	Source          string
}

func NewLogger(options LoggerOptions) *Logger {
	return &Logger{
		options:   options,
		successCh: make(chan struct{}, 1),
	}
}

func (l *Logger) Start(ctx context.Context, logger *slog.Logger) error {
	retryDelay := logRetryInitialDelay

	for {
		err := l.startSingle(ctx, logger)
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}

		logger.Debug("log tail failed, retrying", "error", err, "retry_delay", retryDelay)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(retryDelay):
		}

		retryDelay *= 2
		if retryDelay > logRetryMaxDelay {
			retryDelay = logRetryMaxDelay
		}
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

	var eg errgroup.Group

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logStream, err := l.options.LogStreamFunc(ctx)
	if err != nil {
		return fmt.Errorf("failed to get log stream: %w", err)
	}

	defer func() {
		if closeErr := logStream.Close(); closeErr != nil {
			logger.Warn("🔶 Failed to close log stream", "error", closeErr)
		}
	}()

	eg.Go(func() error {
		defer cancel()

		return tailLogs(ctx, logStream, logCh)
	})

	eg.Go(func() error {
		defer cancel()

		return l.handleLogs(ctx, logCh, logger)
	})

	if err = eg.Wait(); err != nil {
		return fmt.Errorf("failed to wait for log tailing: %w", err)
	}

	return nil
}

// scanCRLF is a bufio.SplitFunc that splits on \r or \n,
// since rsync uses \r to overwrite progress output in-place.
func scanCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if idx := bytes.IndexAny(data, "\r\n"); idx >= 0 {
		// Treat CRLF as a single delimiter to avoid emitting an empty token.
		if data[idx] == '\r' && idx+1 < len(data) && data[idx+1] == '\n' {
			return idx + 2, data[:idx], nil
		}

		return idx + 1, data[:idx], nil
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

func FindLast(text string, parseLine ParseLineFunc) Update {
	if parseLine == nil {
		return Update{}
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Split(scanCRLF)

	var latest Update

	for scanner.Scan() {
		progress, err := parseLine(scanner.Text())
		if err == nil {
			latest = progress
		}
	}

	return latest
}

func tailLogs(ctx context.Context, stream io.Reader, logCh chan<- string) error {
	scanner := bufio.NewScanner(stream)
	scanner.Split(scanCRLF)

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

				continue
			}

			if err := scanner.Err(); err != nil {
				return fmt.Errorf("scan log stream: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err() //nolint:wrapcheck
			case <-time.After(100 * time.Millisecond): //nolint:mnd
			}
		}
	}
}

//nolint:cyclop,funlen
func (l *Logger) handleLogs(ctx context.Context, logCh <-chan string, logger *slog.Logger) error {
	var progressBar *progressbar.ProgressBar

	if l.options.ShowProgressBar {
		progressBar = progressbar.NewOptions64(
			1,
			progressbar.OptionSetWriter(l.options.Writer),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionFullWidth(),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintln(l.options.Writer)
			}),
			progressbar.OptionSetDescription("📂 Copying data..."),
		)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case <-l.successCh:
			if l.options.ShowProgressBar {
				if err := progressBar.Finish(); err != nil {
					logger.Debug("failed to finish progress bar", "error", err)
				}
			}

			return nil
		case logLine := <-logCh:
			if l.options.ParseLineFunc == nil {
				continue
			}

			progress, err := l.options.ParseLineFunc(logLine)
			if err != nil {
				logger.Log(ctx, slog.LevelDebug-1, "failed to parse progress line", "error", err)

				continue
			}

			if !l.options.ShowProgressBar {
				args := []any{
					slog.Group(
						"progress",
						"transferred",
						progress.Transferred,
						"total",
						progress.Total,
						"percentage",
						progress.Percentage,
					),
				}

				if l.options.Source != "" {
					args = append(args, slog.String("source", l.options.Source))
				}

				logger.Debug(logLine, args...)
			} else {
				if err = updateProgressBar(progressBar, progress.Transferred, progress.Total); err != nil {
					logger.Warn("🔶 Failed to update progress bar", "error", err, "progress", progress)
				}
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
