package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	FlagLogLevel  = "log-level"
	FlagLogFormat = "log-format"

	logFormatText = "text"
	logFormatJSON = "json"
)

//nolint:funlen
func buildRootCmd(ctx context.Context, version string, commit string, date string) *cobra.Command {
	cmd := cobra.Command{
		Use:     appName,
		Short:   "A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another",
		Version: fmt.Sprintf("%s (commit: %s) (build date: %s)", version, commit, date),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()
			loglvl, _ := f.GetString(FlagLogLevel)
			logfmt, _ := f.GetString(FlagLogFormat)

			var level slog.Level
			var logHandler slog.Handler

			if err := level.UnmarshalText([]byte(loglvl)); err != nil {
				return fmt.Errorf("failed to parse log level: %w", err)
			}

			handlerOpts := &slog.HandlerOptions{
				Level: level,
			}

			switch logfmt {
			case logFormatJSON:
				logHandler = slog.NewJSONHandler(os.Stderr, handlerOpts)
			case logFormatText, "fancy":
				logHandler = slog.NewTextHandler(os.Stderr, handlerOpts)
			default:
				return fmt.Errorf("unknown log format: %s", logfmt)
			}

			logger := slog.New(logHandler)

			slog.SetLogLoggerLevel(level)
			slog.SetDefault(logger)

			return nil
		},
	}

	cmd.SetContext(ctx)

	levels := []string{
		slog.LevelDebug.String(),
		slog.LevelInfo.String(),
		slog.LevelWarn.String(),
		slog.LevelError.String(),
	}
	formats := []string{
		logFormatText,
		logFormatJSON,
	}

	pf := cmd.PersistentFlags()
	pf.String(FlagLogLevel, slog.LevelInfo.String(),
		"log level, must be one of \""+strings.Join(levels, ", ")+
			"\" or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText")
	pf.String(FlagLogFormat, logFormatText,
		"log format, must be one of: "+strings.Join(formats, ", "))

	logger := slog.Default()

	cmd.AddCommand(buildMigrateCmd(ctx, logger))
	cmd.AddCommand(buildCompletionCmd())

	_ = cmd.RegisterFlagCompletionFunc(FlagLogLevel, buildStaticSliceCompletionFunc(levels))
	_ = cmd.RegisterFlagCompletionFunc(FlagLogFormat, buildStaticSliceCompletionFunc(formats))

	return &cmd
}
