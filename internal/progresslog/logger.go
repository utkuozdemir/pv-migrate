package progresslog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
)

const (
	logRetryInitialDelay = 250 * time.Millisecond
	logRetryMaxDelay     = 5 * time.Second

	// barMaximum is what the bar is drawn against. The data movers state a
	// percentage, so the bar counts percent and its end never moves.
	barMaximum = 100

	// barStreamedMaximum is as far as a reported percentage may drive the bar.
	// Reaching the end finishes the bar for good in the library, and rclone can
	// report everything transferred and then find more work. Completion is left
	// to the caller, which knows whether the job itself finished.
	barStreamedMaximum = barMaximum - 1

	barDescription = "📂 Copying data..."
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

	// Owned by the single goroutine that handles a stream's lines. Stream
	// attempts are sequential, so no locking is needed.
	progressBar    *progressbar.ProgressBar
	barTransferred int64
	barFinished    bool
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

//nolint:cyclop
func (l *Logger) Start(ctx context.Context, logger *slog.Logger) error {
	retryDelay := logRetryInitialDelay

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-l.successCh:
			return nil
		default:
		}

		err := l.startSingle(ctx, logger)
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}

		if errors.Is(err, io.EOF) {
			logger.Debug("log stream ended, retrying", "retry_delay", retryDelay)
		} else {
			logger.Debug("log tail failed, retrying", "error", err, "retry_delay", retryDelay)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-l.successCh:
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

// Rendered reports whether the bar has drawn anything. A bar that never got a
// progress update never rendered, so nothing needs to terminate its line.
func (l *Logger) Rendered() bool {
	return l.barTransferred > 0 || l.barFinished
}

// FinishBar completes the bar's rendering. The in-stream completion signal can
// be consumed by the retry loop instead of the render loop, so a caller that
// joined the goroutines calls this to finish the bar deterministically. Only
// safe once the follower has stopped, and only meaningful once the bar drew
// something.
func (l *Logger) FinishBar(logger *slog.Logger) {
	if l.progressBar == nil || l.barFinished || l.barTransferred == 0 {
		return
	}

	l.barFinished = true

	if err := l.progressBar.Finish(); err != nil {
		logger.Debug("failed to finish progress bar", "error", err)
	}
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

		l.handleLogs(ctx, logCh, logger)

		return nil
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

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case logCh <- scanner.Text():
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan log stream: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	default:
		return io.EOF
	}
}

func (l *Logger) handleLogs(ctx context.Context, logCh <-chan string, logger *slog.Logger) {
	progressBar := l.progressBarOnce()

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.successCh:
			if progressBar != nil && !l.barFinished {
				l.barFinished = true

				if err := progressBar.Finish(); err != nil {
					logger.Debug("failed to finish progress bar", "error", err)
				}
			}

			return
		case logLine := <-logCh:
			l.handleLine(ctx, progressBar, logLine, logger)
		}
	}
}

func (l *Logger) handleLine(
	ctx context.Context,
	progressBar *progressbar.ProgressBar,
	logLine string,
	logger *slog.Logger,
) {
	if l.options.ParseLineFunc == nil {
		return
	}

	progress, err := l.options.ParseLineFunc(logLine)
	if err != nil {
		logger.Log(ctx, slog.LevelDebug-1, "failed to parse progress line", "error", err)

		return
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

		return
	}

	// Monotonic on purpose: the transferred count never legitimately goes
	// backward within one transfer, so anything lower is a stray line, and
	// moving a bar that already completed would strand its finished render.
	if progress.Transferred < l.barTransferred {
		return
	}

	l.barTransferred = progress.Transferred

	if err = l.updateProgressBar(progressBar, progress.Percentage); err != nil {
		logger.Warn("🔶 Failed to update progress bar", "error", err, "progress", progress)
	}
}

// progressBarOnce returns the transfer's one progress bar, creating it on first
// use. One bar for the Logger's whole lifetime rather than one per stream
// attempt: an ended stream is retried, and a fresh bar per retry would render
// its blank state over the finished one and restart the rate estimate.
func (l *Logger) progressBarOnce() *progressbar.ProgressBar {
	if !l.options.ShowProgressBar {
		return nil
	}

	if l.progressBar == nil {
		l.progressBar = progressbar.NewOptions64(
			barMaximum,
			progressbar.OptionSetWriter(l.options.Writer),
			progressbar.OptionEnableColorCodes(true),
			// An ordinary frame becomes one write, so that a log record cannot
			// arrive between a clear and a paint and be painted over. The frame
			// that completes the bar is still followed by a separate newline
			// below, which is the one gap left.
			progressbar.OptionUseANSICodes(paintsInOneWrite(l.options.Writer)),
			progressbar.OptionFullWidth(),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintln(l.options.Writer)
			}),
			progressbar.OptionSetDescription(barDescription),
		)
	}

	return l.progressBar
}

// updateProgressBar moves the bar to the percentage the data mover last
// reported. The bar counts percent rather than bytes because that is the one
// figure both movers state outright: rsync gives no total to count against, and
// reconstructing one from its rounded percentage produced an estimate that was
// wrong by up to a factor of two on the first sample and had to be revised for
// the rest of the transfer.
// paintsInOneWrite reports whether the bar may paint a frame as a single write
// that erases the rest of the line.
//
// That needs a terminal which reads the erase sequence, and Windows is left out
// of it: a console there may not interpret one, and the bar would leave the
// tail of its previous frame on screen instead of clearing it. The two-write
// clear it falls back to is what shipped before, so nothing is worse there than
// it was.
func paintsInOneWrite(writer io.Writer) bool {
	if runtime.GOOS == "windows" {
		return false
	}

	file, ok := writer.(*os.File)

	return ok && isatty.IsTerminal(file.Fd())
}

func (l *Logger) updateProgressBar(progressBar *progressbar.ProgressBar, percentage int) error {
	if err := progressBar.Set64(int64(min(percentage, barStreamedMaximum))); err != nil {
		return fmt.Errorf("failed to set progress bar value: %w", err)
	}

	return nil
}
