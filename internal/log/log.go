package log

import (
	"context"
	"fmt"
	"os"

	"github.com/kyokomi/emoji/v2"
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

func New() (*log.Entry, error) {
	configureGlobalLogger()

	l := log.New()
	l.SetOutput(os.Stdout)

	e := l.WithContext(context.Background())
	err := Configure(e, LevelInfo, FormatFancy)
	if err != nil {
		return nil, err
	}

	return e, nil
}

func Configure(e *log.Entry, level string, format string) error {
	l := e.Logger
	l.SetOutput(os.Stdout)
	formatter, err := getLogFormatter(format)
	if err != nil {
		return err
	}

	logLevel, err := getLogLevel(level)
	if err != nil {
		return err
	}

	l.SetFormatter(formatter)
	l.SetLevel(logLevel)
	e.Context = context.WithValue(e.Context, FormatContextKey, format)

	return nil
}

func getLogFormatter(format string) (log.Formatter, error) {
	switch format {
	case FormatJSON:
		return &log.JSONFormatter{}, nil
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

type fancyFormatter struct{}

func (f *fancyFormatter) Format(e *log.Entry) ([]byte, error) {
	msg := emoji.Sprintf("%s\n", e.Message)
	bytes := []byte(msg)

	return bytes, nil
}

func configureGlobalLogger() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		PadLevelText:  true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
}
