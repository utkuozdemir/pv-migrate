package log

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/forPelevin/gomoji"
	log "github.com/sirupsen/logrus"
)

type LoggerContextKey string

const (
	FormatJSON  = "json"
	FormatFancy = "fancy"

	LevelTrace = "trace"
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelFatal = "fatal"
	LevelPanic = "panic"

	FormatContextKey LoggerContextKey = "log-format"
)

var (
	Formats = []string{FormatJSON, FormatFancy}
	Levels  = []string{
		LevelTrace, LevelDebug, LevelInfo, LevelWarn,
		LevelError, LevelFatal, LevelPanic,
	}
)

func New(ctx context.Context) (*log.Entry, error) {
	configureGlobalLogger()

	l := log.New()
	l.SetOutput(os.Stdout)

	entry := l.WithContext(ctx)

	err := Configure(entry, LevelInfo, FormatFancy)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

func Configure(entry *log.Entry, level string, format string) error {
	logger := entry.Logger
	logger.SetOutput(os.Stdout)

	formatter, err := getLogFormatter(format)
	if err != nil {
		return err
	}

	logLevel, err := getLogLevel(level)
	if err != nil {
		return err
	}

	logger.SetFormatter(formatter)
	logger.SetLevel(logLevel)

	entry.Context = context.WithValue(entry.Context, FormatContextKey, format)

	return nil
}

//nolint:ireturn,nolintlint
func getLogFormatter(format string) (log.Formatter, error) {
	switch format {
	case FormatJSON:
		return &jsonFormatter{}, nil
	case FormatFancy:
		return &fancyFormatter{}, nil
	}

	return nil, fmt.Errorf("unknown log format: %s", format)
}

func getLogLevel(level string) (log.Level, error) {
	switch level {
	case LevelTrace:
		return log.TraceLevel, nil
	case LevelDebug:
		return log.DebugLevel, nil
	case LevelInfo:
		return log.InfoLevel, nil
	case LevelWarn:
		return log.WarnLevel, nil
	case LevelError:
		return log.ErrorLevel, nil
	case LevelFatal:
		return log.FatalLevel, nil
	case LevelPanic:
		return log.PanicLevel, nil
	}

	return 0, fmt.Errorf("unknown log level: %s", level)
}

type jsonFormatter struct {
	inner log.JSONFormatter
}

func (f *jsonFormatter) Format(e *log.Entry) ([]byte, error) {
	e.Message = strings.TrimSpace(gomoji.RemoveEmojis(e.Message))

	formatted, err := f.inner.Format(e)
	if err != nil {
		return nil, fmt.Errorf("failed to format log entry: %w", err)
	}

	return formatted, nil
}

type fancyFormatter struct{}

func (f *fancyFormatter) Format(e *log.Entry) ([]byte, error) {
	return []byte(e.Message), nil
}

func configureGlobalLogger() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		PadLevelText:  true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
}
