package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/utkuozdemir/pv-migrate/internal/util"
	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

const (
	FlagLogLevel  = "log-level"
	FlagLogFormat = "log-format"

	logFormatText = "text"
	logFormatJSON = "json"

	FlagSource           = "source"
	FlagSourceKubeconfig = "source-kubeconfig"
	FlagSourceContext    = "source-context"
	FlagSourceNamespace  = "source-namespace"
	FlagSourcePath       = "source-path"

	FlagDest                = "dest"
	FlagDestKubeconfig      = "dest-kubeconfig"
	FlagDestContext         = "dest-context"
	FlagDestNamespace       = "dest-namespace"
	FlagDestPath            = "dest-path"
	FlagDestHostOverride    = "dest-host-override"
	FlagLoadBalancerTimeout = "loadbalancer-timeout"

	FlagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	FlagIgnoreMounted             = "ignore-mounted"
	FlagNoChown                   = "no-chown"
	FlagNoCleanup                 = "no-cleanup"
	FlagShowProgressBar           = "show-progress-bar"
	FlagSourceMountReadOnly       = "source-mount-read-only"
	FlagStrategies                = "strategies"
	FlagSSHKeyAlgorithm           = "ssh-key-algorithm"
	FlagCompress                  = "compress"

	FlagHelmTimeout   = "helm-timeout"
	FlagHelmValues    = "helm-values"
	FlagHelmSet       = "helm-set"
	FlagHelmSetString = "helm-set-string"
	FlagHelmSetFile   = "helm-set-file"
)

var completionFuncNoFileComplete = func(*cobra.Command, []string,
	string,
) ([]cobra.Completion, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

type Options struct {
	LogLevel  string
	LogFormat string

	Migration pvmigrate.Migration

	// intermediate fields for cobra flag binding
	strategies   []string
	keyAlgorithm string
}

func BuildMigrateCmd(ctx context.Context, version, commit, date string) (*cobra.Command, error) {
	versionStr := fmt.Sprintf("%s (commit: %s) (build date: %s)", version, commit, date)
	use := fmt.Sprintf(
		"%s [--%s=<source-ns>] --%s=<source-pvc> [--%s=<dest-ns>] --%s=<dest-pvc>",
		appName, FlagSourceNamespace, FlagSource, FlagDestNamespace, FlagDest,
	)

	writer := os.Stderr
	isATTY := isatty.IsTerminal(writer.Fd())
	migration := pvmigrate.NewMigration()

	migration.ShowProgressBar = isATTY

	options := Options{
		LogLevel:     slog.LevelInfo.String(),
		LogFormat:    logFormatText,
		Migration:    migration,
		strategies:   util.ConvertStrings[string](migration.Strategies),
		keyAlgorithm: string(migration.KeyAlgorithm),
	}

	cmd := cobra.Command{
		Use:     use,
		Short:   "Migrate data from one Kubernetes PersistentVolumeClaim to another",
		Args:    cobra.NoArgs,
		Version: versionStr,
		RunE: func(cmd *cobra.Command, _ []string) error { //nolint:contextcheck
			return runMigration(cmd, &options, writer, isATTY)
		},
	}

	logLevels := []string{
		slog.LevelDebug.String(),
		slog.LevelInfo.String(),
		slog.LevelWarn.String(),
		slog.LevelError.String(),
	}
	logFormats := []string{
		logFormatText,
		logFormatJSON,
	}

	if err := setMigrateCmdFlags(&cmd, &options, logLevels, logFormats); err != nil {
		return nil, fmt.Errorf("failed to set flags: %w", err)
	}

	if err := setMigrateCmdCompletion(ctx, &cmd, logLevels, logFormats); err != nil {
		return nil, fmt.Errorf("failed to set completion: %w", err)
	}

	cmd.AddCommand(buildCompletionCmd())

	return &cmd, nil
}

func setMigrateCmdCompletion(
	ctx context.Context,
	cmd *cobra.Command,
	levels, formats []string,
) error {
	completions := []struct {
		flag string
		fn   cobra.CompletionFunc
	}{
		{FlagLogLevel, buildStaticSliceCompletionFunc(levels)},
		{FlagLogFormat, buildStaticSliceCompletionFunc(formats)},
		{FlagSourceContext, buildKubeContextCompletionFunc(FlagSourceKubeconfig)},
		{FlagSourceNamespace, buildKubeNSCompletionFunc(ctx, FlagSourceKubeconfig, FlagSourceContext)},
		{FlagSourcePath, completionFuncNoFileComplete},
		{FlagDestContext, buildKubeContextCompletionFunc(FlagDestKubeconfig)},
		{FlagDestNamespace, buildKubeNSCompletionFunc(ctx, FlagDestKubeconfig, FlagDestContext)},
		{FlagDestPath, completionFuncNoFileComplete},
		{FlagStrategies, buildSliceCompletionFunc(util.ConvertStrings[string](pvmigrate.AllStrategies))},
		{FlagSSHKeyAlgorithm, buildStaticSliceCompletionFunc(util.ConvertStrings[string](pvmigrate.KeyAlgorithms))},
		{FlagHelmSet, completionFuncNoFileComplete},
		{FlagHelmSetString, completionFuncNoFileComplete},
		{FlagHelmSetFile, completionFuncNoFileComplete},
		{FlagSource, buildPVCCompletionFunc(ctx, false)},
		{FlagDest, buildPVCCompletionFunc(ctx, true)},
	}

	for _, c := range completions {
		if err := cmd.RegisterFlagCompletionFunc(c.flag, c.fn); err != nil {
			return fmt.Errorf("failed to register completion for flag %q: %w", c.flag, err)
		}
	}

	return nil
}

//nolint:funlen
func setMigrateCmdFlags(cmd *cobra.Command, options *Options, logLevels, logFormats []string) error {
	persistentFlags := cmd.PersistentFlags()
	flags := cmd.Flags()
	migration := &options.Migration

	persistentFlags.StringVar(&options.LogLevel, FlagLogLevel, options.LogLevel,
		"Log level, one of "+strings.Join(logLevels, ", ")+
			" or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText")
	persistentFlags.StringVar(&options.LogFormat, FlagLogFormat, options.LogFormat,
		"Log format, one of "+strings.Join(logFormats, ", "))

	flags.StringVarP(
		&migration.Source.KubeconfigPath,
		FlagSourceKubeconfig,
		"k",
		options.Migration.Source.KubeconfigPath,
		"Path of the kubeconfig file of the source PVC",
	)
	flags.StringVarP(&migration.Source.Context, FlagSourceContext, "c", migration.Source.Context,
		"Context in the kubeconfig file of the source PVC")
	flags.StringVarP(&migration.Source.Namespace, FlagSourceNamespace, "n", migration.Source.Namespace,
		"Namespace of the source PVC")
	flags.StringVar(&migration.Source.Name, FlagSource, migration.Source.Name, "Source PVC name")

	if err := cmd.MarkFlagRequired(FlagSource); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagSource, err)
	}

	flags.StringVarP(&migration.Source.Path, FlagSourcePath, "p", migration.Source.Path,
		"Filesystem path to migrate in the source PVC")

	flags.StringVarP(&migration.Dest.KubeconfigPath, FlagDestKubeconfig, "K", migration.Dest.KubeconfigPath,
		"Path of the kubeconfig file of the destination PVC")
	flags.StringVarP(&migration.Dest.Context, FlagDestContext, "C", migration.Dest.Context,
		"Context in the kubeconfig file of the destination PVC")
	flags.StringVarP(&migration.Dest.Namespace, FlagDestNamespace, "N", migration.Dest.Namespace,
		"Namespace of the destination PVC")
	flags.StringVar(&migration.Dest.Name, FlagDest, migration.Dest.Name, "Destination PVC name")

	if err := cmd.MarkFlagRequired(FlagDest); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagDest, err)
	}

	flags.StringVarP(&migration.Dest.Path, FlagDestPath, "P", migration.Dest.Path,
		"Filesystem path to migrate in the destination PVC")

	flags.BoolVarP(
		&migration.DeleteExtraneousFiles,
		FlagDestDeleteExtraneousFiles,
		"d",
		migration.DeleteExtraneousFiles,
		"Delete extraneous files on the destination using rsync's --delete flag",
	)
	flags.BoolVarP(&migration.IgnoreMounted, FlagIgnoreMounted, "i", migration.IgnoreMounted,
		"Do not fail if the source or destination PVC is mounted")
	flags.BoolVarP(&migration.NoChown, FlagNoChown, "o", migration.NoChown, "Omit chown during rsync")
	flags.BoolVarP(&migration.NoCleanup, FlagNoCleanup, "x", migration.NoCleanup, "Do not clean up after migration")
	flags.BoolVarP(&migration.ShowProgressBar, FlagShowProgressBar,
		"b", migration.ShowProgressBar, "Show a progress bar during migration")
	flags.Lookup(FlagShowProgressBar).DefValue = "true if stderr is a TTY"
	flags.BoolVarP(&migration.SourceMountReadOnly, FlagSourceMountReadOnly, "R", migration.SourceMountReadOnly,
		"Mount the source PVC in read-only mode")
	flags.StringSliceVarP(&options.strategies, FlagStrategies, "s", options.strategies,
		"Comma-separated list of strategies in order (available: "+
			strings.Join(util.ConvertStrings[string](pvmigrate.AllStrategies), ", ")+")",
	)
	flags.StringVarP(&options.keyAlgorithm, FlagSSHKeyAlgorithm, "a", options.keyAlgorithm,
		"SSH key algorithm, one of "+strings.Join(util.ConvertStrings[string](pvmigrate.KeyAlgorithms), ", "))
	flags.StringVarP(&migration.DestHostOverride, FlagDestHostOverride, "H", migration.DestHostOverride,
		"Override for the rsync destination host over SSH. "+
			"By default, determined by the strategy. "+
			"Has no effect for the mount and local strategies")
	flags.DurationVar(
		&migration.LoadBalancerTimeout,
		FlagLoadBalancerTimeout,
		migration.LoadBalancerTimeout,
		fmt.Sprintf(
			"Timeout for the load balancer to receive an external IP. Only used by the %s strategy",
			pvmigrate.LoadBalancer,
		),
	)
	flags.BoolVar(&migration.Compress, FlagCompress, migration.Compress,
		"Compress data during migration (rsync -z)")

	flags.DurationVarP(&migration.HelmTimeout, FlagHelmTimeout, "t", migration.HelmTimeout,
		"Helm install/uninstall timeout")
	flags.StringSliceVarP(&migration.HelmValuesFiles, FlagHelmValues, "f", migration.HelmValuesFiles,
		"Additional Helm values files (YAML file or URL, can specify multiple)")
	flags.StringSliceVar(&migration.HelmValues, FlagHelmSet, migration.HelmValues,
		"Additional Helm values (key1=val1,key2=val2)")
	flags.StringSliceVar(&migration.HelmStringValues, FlagHelmSetString, migration.HelmStringValues,
		"Additional Helm string values (key1=val1,key2=val2)")
	flags.StringSliceVar(&migration.HelmFileValues, FlagHelmSetFile, migration.HelmFileValues,
		"Additional Helm values from files (key1=path1,key2=path2)")

	return nil
}

func runMigration(cmd *cobra.Command, options *Options, writer io.Writer, isATTY bool) error {
	ctx := cmd.Context()

	logger, err := buildLogger(options.LogLevel, options.LogFormat, isATTY)
	if err != nil {
		return fmt.Errorf("failed to build logger: %w", err)
	}

	options.Migration.Strategies = util.ConvertStrings[pvmigrate.Strategy](options.strategies)
	options.Migration.KeyAlgorithm = pvmigrate.KeyAlgorithm(options.keyAlgorithm)
	options.Migration.Writer = writer
	options.Migration.Logger = logger

	logger.Info("🚀 Starting migration")

	if options.Migration.DeleteExtraneousFiles {
		logger.Info("❕ Extraneous files will be deleted from the destination")
	}

	if err = pvmigrate.Run(ctx, &options.Migration); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

func buildLogger(logLevel, logFormat string, isATTY bool) (*slog.Logger, error) {
	var (
		level   slog.Level
		handler slog.Handler
	)

	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, fmt.Errorf("failed to parse log level: %w", err)
	}

	writer := os.Stderr

	switch logFormat {
	case logFormatJSON:
		handler = slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	case logFormatText:
		handler = tint.NewHandler(writer, &tint.Options{
			Level:   level,
			NoColor: !isATTY,
		})
	default:
		return nil, fmt.Errorf("unknown log format: %s", logFormat)
	}

	logger := slog.New(handler)

	slog.SetLogLoggerLevel(level)
	slog.SetDefault(logger)

	return logger, nil
}
