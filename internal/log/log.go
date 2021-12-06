package log

import (
	"context"
	"fmt"
	"github.com/kyokomi/emoji/v2"
	log "github.com/sirupsen/logrus"
	"os"
)

type LoggerContextKey string

const (
	FormatJson  = "json"
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
	Formats = []string{FormatJson, FormatFancy}
	Levels  = []string{LevelTrace, LevelDebug, LevelInfo, LevelWarn,
		LevelError, LevelFatal, LevelPanic}
)

func BuildLogger(logger *log.Logger, level string, format string) (*log.Entry, error) {
	logger.SetOutput(os.Stdout)
	formatter, err := getLogFormatter(format)
	if err != nil {
		return nil, err
	}

	logLevel, err := getLogLevel(level)
	if err != nil {
		return nil, err
	}

	logger.SetFormatter(formatter)
	logger.SetLevel(logLevel)
	ctx := context.WithValue(context.Background(), FormatContextKey, format)
	return logger.WithContext(ctx), nil
}

func getLogFormatter(format string) (log.Formatter, error) {
	switch format {
	case FormatJson:
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

type fancyFormatter struct {
}

func (f *fancyFormatter) Format(e *log.Entry) ([]byte, error) {
	logErr := extractLogError(e)

	errStr := ""
	if logErr != nil {
		errStr = fmt.Sprintf(" - Error: %s", logErr)
	}

	msg := emoji.Sprintf("%s%s\n", e.Message, errStr)
	bytes := []byte(msg)
	return bytes, nil
}

func extractLogError(e *log.Entry) error {
	logErr, exists := e.Data[log.ErrorKey]
	if !exists {
		return nil
	}

	err, ok := logErr.(error)
	if !ok {
		return nil
	}

	return err
}
