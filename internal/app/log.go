package app

import (
	"fmt"
	"github.com/kyokomi/emoji/v2"
	log "github.com/sirupsen/logrus"
	"os"
)

const (
	logFormatJson  = "json"
	logFormatFancy = "fancy"

	logLevelTrace = "trace"
	logLevelDebug = "debug"
	logLevelInfo  = "info"
	logLevelWarn  = "warn"
	logLevelError = "error"
	logLevelFatal = "fatal"
	logLevelPanic = "panic"
)

var (
	logFormats = []string{logFormatJson, logFormatFancy}
	logLevels  = []string{logLevelTrace, logLevelDebug, logLevelInfo, logLevelWarn,
		logLevelError, logLevelFatal, logLevelPanic}
)

func configureLogging(level string, format string) error {
	log.SetOutput(os.Stdout)
	formatter, err := getLogFormatter(format)
	if err != nil {
		return err
	}

	logLevel, err := getLogLevel(level)
	if err != nil {
		return err
	}

	log.SetFormatter(formatter)
	log.SetLevel(logLevel)
	return nil
}

func getLogFormatter(format string) (log.Formatter, error) {
	switch format {
	case logFormatJson:
		return &log.JSONFormatter{}, nil
	case logFormatFancy:
		return &fancyFormatter{}, nil
	}
	return nil, fmt.Errorf("unknown log format: %s", format)
}

func getLogLevel(level string) (log.Level, error) {
	switch level {
	case logLevelTrace:
		return log.TraceLevel, nil
	case logLevelDebug:
		return log.DebugLevel, nil
	case logLevelInfo:
		return log.InfoLevel, nil
	case logLevelWarn:
		return log.WarnLevel, nil
	case logLevelError:
		return log.ErrorLevel, nil
	case logLevelFatal:
		return log.FatalLevel, nil
	case logLevelPanic:
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
