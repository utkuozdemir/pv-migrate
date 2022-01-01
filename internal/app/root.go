package app

import (
	"fmt"
	"github.com/spf13/cobra"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"strings"
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			loglvl, _ := f.GetString(FlagLogLevel)
			logfmt, _ := f.GetString(FlagLogFormat)
			err := applog.Configure(logger, loglvl, logfmt)
			if err != nil {
				return err
			}

			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.String(FlagLogLevel, applog.LevelInfo,
		fmt.Sprintf("log level, must be one of: %s", strings.Join(applog.Levels, ", ")))
	pf.String(FlagLogFormat, applog.FormatFancy,
		fmt.Sprintf("log format, must be one of: %s", strings.Join(applog.Formats, ", ")))

	cmd.AddCommand(buildMigrateCmd())
	cmd.AddCommand(buildComplectionCmd())

	_ = cmd.RegisterFlagCompletionFunc(FlagLogLevel, buildStaticSliceCompletionFunc(applog.Levels))
	_ = cmd.RegisterFlagCompletionFunc(FlagLogFormat, buildStaticSliceCompletionFunc(applog.Formats))

	return &cmd
}
