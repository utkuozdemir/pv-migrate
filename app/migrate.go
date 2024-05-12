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
	flag "github.com/spf13/pflag"

	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/migrator"
	"github.com/utkuozdemir/pv-migrate/rsync/progress"
	"github.com/utkuozdemir/pv-migrate/ssh"
	"github.com/utkuozdemir/pv-migrate/strategy"
)

const (
	CommandMigrate = "migrate"

	FlagLogLevel  = "log-level"
	FlagLogFormat = "log-format"

	logFormatText = "text"
	logFormatJSON = "json"

	FlagSource           = "source"
	FlagSourceKubeconfig = "source-kubeconfig"
	FlagSourceContext    = "source-context"
	FlagSourceNamespace  = "source-namespace"
	FlagSourcePath       = "source-path"

	FlagDest             = "dest"
	FlagDestKubeconfig   = "dest-kubeconfig"
	FlagDestContext      = "dest-context"
	FlagDestNamespace    = "dest-namespace"
	FlagDestPath         = "dest-path"
	FlagDestHostOverride = "dest-host-override"
	FlagLBSvcTimeout     = "lbsvc-timeout"

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

	lbSvcTimeoutDefault = 2 * time.Minute
)

var completionFuncNoFileComplete = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func BuildMigrateCmd(ctx context.Context, version, commit, date string, legacy bool) *cobra.Command {
	var (
		args              cobra.PositionalArgs
		versionStr        string
		aliases           []string
		use               string
		validArgsFunction func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
		hidden            bool
	)

	if legacy {
		args = cobra.ExactArgs(2) //nolint:mnd
		aliases = []string{"m"}
		use = CommandMigrate + " <source-pvc> <dest-pvc>"
		validArgsFunction = buildLegacyPVCsCompletionFunc(ctx)
		hidden = true
	} else {
		args = cobra.NoArgs
		versionStr = fmt.Sprintf("%s (commit: %s) (build date: %s)", version, commit, date)
		use = fmt.Sprintf(
			"%s [--%s=<source-ns>] --%s=<source-pvc> [--%s=<dest-ns>] --%s=<dest-pvc>",
			appName, FlagSourceNamespace, FlagSource, FlagDestNamespace, FlagDest,
		)
	}

	cmd := cobra.Command{
		Use:               use,
		Aliases:           aliases,
		Short:             "Migrate data from one Kubernetes PersistentVolumeClaim to another",
		Args:              args,
		ValidArgsFunction: validArgsFunction,
		Version:           versionStr,
		RunE:              runMigration,
		Hidden:            hidden,
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

	setMigrateCmdFlags(&cmd, logLevels, logFormats, legacy)
	setMigrateCmdCompletion(ctx, &cmd, logLevels, logFormats, legacy)

	if !legacy {
		legacyMigrateCommand := BuildMigrateCmd(ctx, version, commit, date, true)

		cmd.AddCommand(legacyMigrateCommand)
	}

	cmd.AddCommand(buildCompletionCmd())

	return &cmd
}

//nolint:errcheck
func setMigrateCmdCompletion(ctx context.Context, cmd *cobra.Command, levels, formats []string, legacy bool) {
	cmd.RegisterFlagCompletionFunc(FlagLogLevel, buildStaticSliceCompletionFunc(levels))
	cmd.RegisterFlagCompletionFunc(FlagLogFormat, buildStaticSliceCompletionFunc(formats))

	cmd.RegisterFlagCompletionFunc(FlagSourceContext,
		buildKubeContextCompletionFunc(FlagSourceKubeconfig))
	cmd.RegisterFlagCompletionFunc(FlagSourceNamespace,
		buildKubeNSCompletionFunc(ctx, FlagSourceKubeconfig, FlagSourceContext))
	cmd.RegisterFlagCompletionFunc(FlagSourcePath, completionFuncNoFileComplete)

	cmd.RegisterFlagCompletionFunc(FlagDestContext,
		buildKubeContextCompletionFunc(FlagDestKubeconfig))
	cmd.RegisterFlagCompletionFunc(FlagDestNamespace,
		buildKubeNSCompletionFunc(ctx, FlagDestKubeconfig, FlagDestContext))
	cmd.RegisterFlagCompletionFunc(FlagDestPath, completionFuncNoFileComplete)

	cmd.RegisterFlagCompletionFunc(FlagStrategies, buildSliceCompletionFunc(strategy.AllStrategies))
	cmd.RegisterFlagCompletionFunc(FlagSSHKeyAlgorithm, buildStaticSliceCompletionFunc(ssh.KeyAlgorithms))

	cmd.RegisterFlagCompletionFunc(FlagHelmSet, completionFuncNoFileComplete)
	cmd.RegisterFlagCompletionFunc(FlagHelmSetString, completionFuncNoFileComplete)
	cmd.RegisterFlagCompletionFunc(FlagHelmSetFile, completionFuncNoFileComplete)

	if !legacy {
		cmd.RegisterFlagCompletionFunc(FlagSource, buildPVCCompletionFunc(ctx, false))
		cmd.RegisterFlagCompletionFunc(FlagDest, buildPVCCompletionFunc(ctx, true))
	}
}

//nolint:funlen
func setMigrateCmdFlags(cmd *cobra.Command, logLevels, logFormats []string, legacy bool) {
	persistentFlags := cmd.PersistentFlags()
	flags := cmd.Flags()

	persistentFlags.String(FlagLogLevel, slog.LevelInfo.String(),
		"log level, must be one of \""+strings.Join(logLevels, ", ")+
			"\" or an slog-parseable level: https://pkg.go.dev/log/slog#Level.UnmarshalText")
	persistentFlags.String(FlagLogFormat, logFormatText,
		"log format, must be one of: "+strings.Join(logFormats, ", "))

	flags.StringP(FlagSourceKubeconfig, "k", "", "path of the kubeconfig file of the source PVC")
	flags.StringP(FlagSourceContext, "c", "", "context in the kubeconfig file of the source PVC")
	flags.StringP(FlagSourceNamespace, "n", "", "namespace of the source PVC")

	if !legacy {
		flags.String(FlagSource, "", "source PVC name")

		cmd.MarkFlagRequired(FlagSource) //nolint:errcheck
	}

	flags.StringP(FlagSourcePath, "p", "/", "the filesystem path to migrate in the source PVC")

	flags.StringP(FlagDestKubeconfig, "K", "", "path of the kubeconfig file of the destination PVC")
	flags.StringP(FlagDestContext, "C", "", "context in the kubeconfig file of the destination PVC")
	flags.StringP(FlagDestNamespace, "N", "", "namespace of the destination PVC")

	if !legacy {
		flags.String(FlagDest, "", "destination PVC name")

		cmd.MarkFlagRequired(FlagDest) //nolint:errcheck
	}

	flags.StringP(FlagDestPath, "P", "/", "the filesystem path to migrate in the destination PVC")

	flags.BoolP(FlagDestDeleteExtraneousFiles, "d", false,
		"delete extraneous files on the destination by using rsync's '--delete' flag")
	flags.BoolP(FlagIgnoreMounted, "i", false,
		"do not fail if the source or destination PVC is mounted")
	flags.BoolP(FlagNoChown, "o", false, "omit chown on rsync")
	flags.BoolP(FlagSkipCleanup, "x", false, "skip cleanup of the migration")
	flags.BoolP(FlagNoProgressBar, "b", false, "do not display a progress bar")
	flags.BoolP(FlagSourceMountReadOnly, "R", true, "mount the source PVC in ReadOnly mode")
	flags.StringSliceP(FlagStrategies, "s", strategy.DefaultStrategies,
		"the comma-separated list of strategies to be used in the given order")
	flags.StringP(FlagSSHKeyAlgorithm, "a", ssh.Ed25519KeyAlgorithm,
		"ssh key algorithm to be used. Valid values are "+strings.Join(ssh.KeyAlgorithms, ","))
	flags.StringP(FlagDestHostOverride, "H", "",
		"the override for the rsync host destination when it is run over SSH, "+
			"in cases when you need to target a different destination IP on rsync for some reason. "+
			"By default, it is determined by used strategy and differs across strategies. "+
			"Has no effect for mnt2 and local strategies")
	flags.Duration(FlagLBSvcTimeout, lbSvcTimeoutDefault, fmt.Sprintf("timeout for the load balancer service to "+
		"receive an external IP. Only used by the %s strategy", strategy.LbSvcStrategy))
	flags.Bool(FlagCompress, true, "compress data during migration ('-z' flag of rsync)")

	flags.DurationP(FlagHelmTimeout, "t", 1*time.Minute, "install/uninstall timeout for helm releases")
	flags.StringSliceP(FlagHelmValues, "f", nil,
		"set additional Helm values by a YAML file or a URL (can specify multiple)")
	flags.StringSlice(FlagHelmSet, nil, "set additional Helm values on the command line (can specify "+
		"multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringSlice(FlagHelmSetString, nil, "set additional Helm STRING values on the command line "+
		"(can specify multiple or separate values with commas: key1=val1,key2=val2)")
	flags.StringSlice(FlagHelmSetFile, nil, "set additional Helm values from respective files specified "+
		"via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
}

//nolint:funlen
func runMigration(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()

	ctx := cmd.Context()

	logger, canDisplayProgressBar, err := buildLogger(flags)
	if err != nil {
		return fmt.Errorf("failed to build logger: %w", err)
	}

	if canDisplayProgressBar {
		ctx = context.WithValue(ctx, progress.CanDisplayProgressBarContextKey{}, struct{}{})
	}

	var src, dest string

	//nolint:mnd
	if len(args) == 2 {
		logger.Info(fmt.Sprintf("⚠️ Using legacy mode with PVC names as arguments, "+
			"consider switching to --%s and --%s flags", FlagSource, FlagDest))

		src = args[0]
		dest = args[1]
	} else {
		src, _ = flags.GetString(FlagSource)
		dest, _ = flags.GetString(FlagDest)
	}

	ignoreMounted, _ := flags.GetBool(FlagIgnoreMounted)
	srcMountReadOnly, _ := flags.GetBool(FlagSourceMountReadOnly)
	noChown, _ := flags.GetBool(FlagNoChown)
	skipCleanup, _ := flags.GetBool(FlagSkipCleanup)
	noProgressBar, _ := flags.GetBool(FlagNoProgressBar)
	sshKeyAlg, _ := flags.GetString(FlagSSHKeyAlgorithm)
	helmTimeout, _ := flags.GetDuration(FlagHelmTimeout)
	helmValues, _ := flags.GetStringSlice(FlagHelmValues)
	helmSet, _ := flags.GetStringSlice(FlagHelmSet)
	helmSetString, _ := flags.GetStringSlice(FlagHelmSetString)
	helmSetFile, _ := flags.GetStringSlice(FlagHelmSetFile)
	strs, _ := flags.GetStringSlice(FlagStrategies)
	destHostOverride, _ := flags.GetString(FlagDestHostOverride)
	lbSvcTimeout, _ := flags.GetDuration(FlagLBSvcTimeout)
	compress, _ := flags.GetBool(FlagCompress)

	deleteExtraneousFiles, _ := flags.GetBool(FlagDestDeleteExtraneousFiles)
	request := migration.Request{
		Source:                buildSrcPVCInfo(flags, src),
		Dest:                  buildDestPVCInfo(flags, dest),
		DeleteExtraneousFiles: deleteExtraneousFiles,
		IgnoreMounted:         ignoreMounted,
		SourceMountReadOnly:   srcMountReadOnly,
		NoChown:               noChown,
		SkipCleanup:           skipCleanup,
		NoProgressBar:         noProgressBar,
		KeyAlgorithm:          sshKeyAlg,
		HelmTimeout:           helmTimeout,
		HelmValuesFiles:       helmValues,
		HelmValues:            helmSet,
		HelmStringValues:      helmSetString,
		HelmFileValues:        helmSetFile,
		Strategies:            strs,
		DestHostOverride:      destHostOverride,
		LBSvcTimeout:          lbSvcTimeout,
		Compress:              compress,
	}

	logger.Info("🚀 Starting migration")

	if deleteExtraneousFiles {
		logger.Info("❕ Extraneous files will be deleted from the destination")
	}

	if err := migrator.New().Run(ctx, &request, logger); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

//nolint:nonamedreturns
func buildLogger(flags *flag.FlagSet) (logger *slog.Logger, canDisplayProgressBar bool, err error) {
	loglvl, _ := flags.GetString(FlagLogLevel)
	logfmt, _ := flags.GetString(FlagLogFormat)

	var (
		level   slog.Level
		handler slog.Handler
	)

	if err = level.UnmarshalText([]byte(loglvl)); err != nil {
		return nil, false, fmt.Errorf("failed to parse log level: %w", err)
	}

	writer := os.Stderr

	switch logfmt {
	case logFormatJSON:
		handler = slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	case logFormatText, "fancy":
		isATTY := isatty.IsTerminal(writer.Fd())

		canDisplayProgressBar = isATTY

		handler = tint.NewHandler(writer, &tint.Options{
			Level:   level,
			NoColor: !isATTY,
		})
	default:
		return nil, false, fmt.Errorf("unknown log format: %s", logfmt)
	}

	logger = slog.New(handler)

	slog.SetLogLoggerLevel(level)
	slog.SetDefault(logger)

	return logger, canDisplayProgressBar, nil
}

func buildSrcPVCInfo(flags *flag.FlagSet, name string) *migration.PVCInfo {
	srcKubeconfigPath, _ := flags.GetString(FlagSourceKubeconfig)
	srcContext, _ := flags.GetString(FlagSourceContext)
	srcNS, _ := flags.GetString(FlagSourceNamespace)
	srcPath, _ := flags.GetString(FlagSourcePath)

	return &migration.PVCInfo{
		KubeconfigPath: srcKubeconfigPath,
		Context:        srcContext,
		Namespace:      srcNS,
		Name:           name,
		Path:           srcPath,
	}
}

func buildDestPVCInfo(flags *flag.FlagSet, name string) *migration.PVCInfo {
	destKubeconfigPath, _ := flags.GetString(FlagDestKubeconfig)
	destContext, _ := flags.GetString(FlagDestContext)
	destNS, _ := flags.GetString(FlagDestNamespace)
	destPath, _ := flags.GetString(FlagDestPath)

	return &migration.PVCInfo{
		KubeconfigPath: destKubeconfigPath,
		Context:        destContext,
		Namespace:      destNS,
		Name:           name,
		Path:           destPath,
	}
}
