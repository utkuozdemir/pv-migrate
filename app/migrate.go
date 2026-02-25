package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/migrator"
	"github.com/utkuozdemir/pv-migrate/rsync/progress"
	"github.com/utkuozdemir/pv-migrate/ssh"
	"github.com/utkuozdemir/pv-migrate/strategy"
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
	FlagSkipCleanup               = "skip-cleanup"
	FlagNoProgressBar             = "no-progress-bar"
	FlagSourceMountReadOnly       = "source-mount-read-only"
	FlagStrategies                = "strategies"
	FlagSSHKeyAlgorithm           = "ssh-key-algorithm"
	FlagCompress                  = "compress"

	FlagHelmTimeout   = "helm-timeout"
	FlagHelmValues    = "helm-values"
	FlagHelmSet       = "helm-set"
	FlagHelmSetString = "helm-set-string"
	FlagHelmSetFile   = "helm-set-file"

	loadBalancerTimeoutDefault = 2 * time.Minute
)

var completionFuncNoFileComplete = func(*cobra.Command, []string,
	string,
) ([]cobra.Completion, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

type MigrationOptions struct {
	LogLevel  string
	LogFormat string

	Request migration.Request
}

func BuildMigrateCmd(ctx context.Context, version, commit, date string) (*cobra.Command, error) {
	versionStr := fmt.Sprintf("%s (commit: %s) (build date: %s)", version, commit, date)
	use := fmt.Sprintf(
		"%s [--%s=<source-ns>] --%s=<source-pvc> [--%s=<dest-ns>] --%s=<dest-pvc>",
		appName, FlagSourceNamespace, FlagSource, FlagDestNamespace, FlagDest,
	)

	var options MigrationOptions

	cmd := cobra.Command{
		Use:     use,
		Short:   "Migrate data from one Kubernetes PersistentVolumeClaim to another",
		Args:    cobra.NoArgs,
		Version: versionStr,
		RunE: func(cmd *cobra.Command, _ []string) error { //nolint:contextcheck
			return runMigration(cmd, &options)
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
		{FlagStrategies, buildSliceCompletionFunc(strategy.AllStrategies)},
		{FlagSSHKeyAlgorithm, buildStaticSliceCompletionFunc(ssh.KeyAlgorithms)},
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
func setMigrateCmdFlags(cmd *cobra.Command, options *MigrationOptions, logLevels, logFormats []string) error {
	persistentFlags := cmd.PersistentFlags()
	flags := cmd.Flags()

	req := &options.Request
	req.Source = &migration.PVCInfo{}
	req.Dest = &migration.PVCInfo{}

	persistentFlags.StringVar(&options.LogLevel, FlagLogLevel, slog.LevelInfo.String(),
		"log level, must be one of \""+strings.Join(logLevels, ", ")+
			"\" or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText")
	persistentFlags.StringVar(&options.LogFormat, FlagLogFormat, logFormatText,
		"log format, must be one of: "+strings.Join(logFormats, ", "))

	flags.StringVarP(&req.Source.KubeconfigPath, FlagSourceKubeconfig, "k", "",
		"path of the kubeconfig file of the source PVC")
	flags.StringVarP(&req.Source.Context, FlagSourceContext, "c", "",
		"context in the kubeconfig file of the source PVC")
	flags.StringVarP(&req.Source.Namespace, FlagSourceNamespace, "n", "",
		"namespace of the source PVC")
	flags.StringVar(&req.Source.Name, FlagSource, "", "source PVC name")

	if err := cmd.MarkFlagRequired(FlagSource); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagSource, err)
	}

	flags.StringVarP(&req.Source.Path, FlagSourcePath, "p", "/",
		"the filesystem path to migrate in the source PVC")

	flags.StringVarP(&req.Dest.KubeconfigPath, FlagDestKubeconfig, "K", "",
		"path of the kubeconfig file of the destination PVC")
	flags.StringVarP(&req.Dest.Context, FlagDestContext, "C", "",
		"context in the kubeconfig file of the destination PVC")
	flags.StringVarP(&req.Dest.Namespace, FlagDestNamespace, "N", "",
		"namespace of the destination PVC")
	flags.StringVar(&req.Dest.Name, FlagDest, "", "destination PVC name")

	if err := cmd.MarkFlagRequired(FlagDest); err != nil {
		return fmt.Errorf("failed to mark flag %q as required: %w", FlagDest, err)
	}

	flags.StringVarP(&req.Dest.Path, FlagDestPath, "P", "/",
		"the filesystem path to migrate in the destination PVC")

	flags.BoolVarP(&req.DeleteExtraneousFiles, FlagDestDeleteExtraneousFiles, "d", false,
		"delete extraneous files on the destination by using rsync's '--delete' flag")
	flags.BoolVarP(&req.IgnoreMounted, FlagIgnoreMounted, "i", false,
		"do not fail if the source or destination PVC is mounted")
	flags.BoolVarP(&req.NoChown, FlagNoChown, "o", false, "omit chown on rsync")
	flags.BoolVarP(&req.SkipCleanup, FlagSkipCleanup, "x", false, "skip cleanup of the migration")
	flags.BoolVarP(&req.NoProgressBar, FlagNoProgressBar, "b", false, "do not display a progress bar")
	flags.BoolVarP(&req.SourceMountReadOnly, FlagSourceMountReadOnly, "R", true,
		"mount the source PVC in ReadOnly mode")
	flags.StringSliceVarP(&req.Strategies, FlagStrategies, "s", strategy.DefaultStrategies,
		"the comma-separated list of strategies to be used in the given order (available: "+
			strings.Join(strategy.AllStrategies, ",")+")",
	)
	flags.StringVarP(&req.KeyAlgorithm, FlagSSHKeyAlgorithm, "a", ssh.Ed25519KeyAlgorithm,
		"ssh key algorithm to be used. Valid values are "+strings.Join(ssh.KeyAlgorithms, ","))
	flags.StringVarP(&req.DestHostOverride, FlagDestHostOverride, "H", "",
		"the override for the rsync host destination when it is run over SSH, "+
			"in cases when you need to target a different destination IP on rsync for some reason. "+
			"By default, it is determined by used strategy and differs across strategies. "+
			"Has no effect for mount and local strategies")
	flags.DurationVar(&req.LoadBalancerTimeout, FlagLoadBalancerTimeout, loadBalancerTimeoutDefault,
		fmt.Sprintf("timeout for the load balancer service to "+
			"receive an external IP. Only used by the %s strategy", strategy.LoadBalancerStrategy),
	)
	flags.BoolVar(&req.Compress, FlagCompress, true,
		"compress data during migration ('-z' flag of rsync)")

	flags.DurationVarP(&req.HelmTimeout, FlagHelmTimeout, "t", 1*time.Minute,
		"install/uninstall timeout for helm releases")
	flags.StringSliceVarP(&req.HelmValuesFiles, FlagHelmValues, "f", nil,
		"set additional Helm values by a YAML file or a URL (can specify multiple)")
	flags.StringSliceVar(&req.HelmValues, FlagHelmSet, nil,
		"set additional Helm values on the command line (can specify "+
			"multiple or separate values with commas: key1=val1,key2=val2)",
	)
	flags.StringSliceVar(&req.HelmStringValues, FlagHelmSetString, nil,
		"set additional Helm STRING values on the command line "+
			"(can specify multiple or separate values with commas: key1=val1,key2=val2)",
	)
	flags.StringSliceVar(&req.HelmFileValues, FlagHelmSetFile, nil,
		"set additional Helm values from respective files specified "+
			"via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)",
	)

	return nil
}

func runMigration(cmd *cobra.Command, options *MigrationOptions) error {
	ctx := cmd.Context()

	logger, canDisplayProgressBar, err := buildLogger(options.LogLevel, options.LogFormat)
	if err != nil {
		return fmt.Errorf("failed to build logger: %w", err)
	}

	if canDisplayProgressBar {
		ctx = context.WithValue(ctx, progress.CanDisplayProgressBarContextKey{}, struct{}{})
	}

	logger.Info("🚀 Starting migration")

	if options.Request.DeleteExtraneousFiles {
		logger.Info("❕ Extraneous files will be deleted from the destination")
	}

	if err := migrator.New().Run(ctx, &options.Request, logger); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

//nolint:nonamedreturns
func buildLogger(logLevel, logFormat string) (logger *slog.Logger, canDisplayProgressBar bool, err error) {
	var (
		level   slog.Level
		handler slog.Handler
	)

	if err = level.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, false, fmt.Errorf("failed to parse log level: %w", err)
	}

	writer := os.Stderr

	switch logFormat {
	case logFormatJSON:
		handler = slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	case logFormatText:
		isATTY := isatty.IsTerminal(writer.Fd())

		canDisplayProgressBar = isATTY

		handler = tint.NewHandler(writer, &tint.Options{
			Level:   level,
			NoColor: !isATTY,
		})
	default:
		return nil, false, fmt.Errorf("unknown log format: %s", logFormat)
	}

	logger = slog.New(handler)

	slog.SetLogLoggerLevel(level)
	slog.SetDefault(logger)

	return logger, canDisplayProgressBar, nil
}
