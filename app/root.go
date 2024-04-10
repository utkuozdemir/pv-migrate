package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	applog "github.com/utkuozdemir/pv-migrate/log"
)

const (
	FlagLogLevel  = "log-level"
	FlagLogFormat = "log-format"
)

func buildRootCmd(version string, commit string, date string) *cobra.Command {
	cmd := cobra.Command{
		Use:     appName,
		Short:   "A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another",
		Version: fmt.Sprintf("%s (commit: %s) (build date: %s)", version, commit, date),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()
			loglvl, _ := f.GetString(FlagLogLevel)
			logfmt, _ := f.GetString(FlagLogFormat)
			err := applog.Configure(logger, loglvl, logfmt)
			if err != nil {
				return fmt.Errorf("failed to configure logger: %w", err)
			}

			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.String(FlagLogLevel, applog.LevelInfo, "log level, must be one of: "+strings.Join(applog.Levels, ", "))
	pf.String(FlagLogFormat, applog.FormatFancy, "log format, must be one of: "+strings.Join(applog.Formats, ", "))

	cmd.AddCommand(buildMigrateCmd())
	cmd.AddCommand(buildCompletionCmd())

	_ = cmd.RegisterFlagCompletionFunc(FlagLogLevel, buildStaticSliceCompletionFunc(applog.Levels))
	_ = cmd.RegisterFlagCompletionFunc(FlagLogFormat, buildStaticSliceCompletionFunc(applog.Formats))

	return &cmd
}
