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
	LogFormatJson  = "json"
	LogFormatFancy = "fancy"

	LogLevelTrace = "trace"
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
	LogLevelFatal = "fatal"
	LogLevelPanic = "panic"

	LogFormatContextKey LoggerContextKey = "log-format"
)

var (
	LogFormats = []string{LogFormatJson, LogFormatFancy}
	LogLevels  = []string{LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarn,
		LogLevelError, LogLevelFatal, LogLevelPanic}
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
	ctx := context.WithValue(context.Background(), LogFormatContextKey, format)
	return logger.WithContext(ctx), nil
}

func getLogFormatter(format string) (log.Formatter, error) {
	switch format {
	case LogFormatJson:
		return &log.JSONFormatter{}, nil
	case LogFormatFancy:
		return &fancyFormatter{}, nil
	}
	return nil, fmt.Errorf("unknown log format: %s", format)
}

func getLogLevel(level string) (log.Level, error) {
	switch level {
	case LogLevelTrace:
		return log.TraceLevel, nil
	case LogLevelDebug:
		return log.DebugLevel, nil
	case LogLevelInfo:
		return log.InfoLevel, nil
	case LogLevelWarn:
		return log.WarnLevel, nil
	case LogLevelError:
		return log.ErrorLevel, nil
	case LogLevelFatal:
		return log.FatalLevel, nil
	case LogLevelPanic:
		return log.PanicLevel, nil
	}

	return 0, fmt.Errorf("unknown log level: %s", level)
}

type fancyFormatter struct {
}

func (f *fancyFormatter) Format(e *log.Entry) ([]byte, error) {
	msg := emoji.Sprintf("%s\n", e.Message)
	bytes := []byte(msg)
	return bytes, nil
}
