package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"

	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/migrator"
	"github.com/utkuozdemir/pv-migrate/ssh"
	"github.com/utkuozdemir/pv-migrate/strategy"
)

const (
	CommandMigrate = "migrate"

	FlagSourceKubeconfig = "source-kubeconfig"
	FlagSourceContext    = "source-context"
	FlagSourceNamespace  = "source-namespace"
	FlagSourcePath       = "source-path"

	FlagDestKubeconfig   = "dest-kubeconfig"
	FlagDestContext      = "dest-context"
	FlagDestNamespace    = "dest-namespace"
	FlagDestPath         = "dest-path"
	FlagDestHostOverride = "dest-host-override"
	FlagLBSvcTimeout     = "lbsvc-timeout"

	FlagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	FlagIgnoreMounted             = "ignore-mounted"
	FlagNoChown                   = "no-chown"
	FlagNoProgressBar             = "no-progress-bar"
	FlagSourceMountReadOnly       = "source-mount-read-only"
	FlagStrategies                = "strategies"
	FlagSSHKeyAlgorithm           = "ssh-key-algorithm"

	FlagHelmTimeout   = "helm-timeout"
	FlagHelmValues    = "helm-values"
	FlagHelmSet       = "helm-set"
	FlagHelmSetString = "helm-set-string"
	FlagHelmSetFile   = "helm-set-file"

	migrateCmdNumArgs = 2

	lbSvcTimeoutDefault = 2 * time.Minute
)

var completionFuncNoFileComplete = func(cmd *cobra.Command, args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func buildMigrateCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:               CommandMigrate + " <source-pvc> <dest-pvc>",
		Aliases:           []string{"m"},
		Short:             "Migrate data from one Kubernetes PersistentVolumeClaim to another",
		Args:              cobra.ExactArgs(migrateCmdNumArgs),
		ValidArgsFunction: buildPVCsCompletionFunc(),
		RunE:              runMigration,
	}

	setMigrateCmdFlags(&cmd)
	setMigrateCmdCompletion(&cmd)

	return &cmd
}

func setMigrateCmdCompletion(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc(FlagSourceContext, buildKubeContextCompletionFunc(FlagSourceKubeconfig))
	_ = cmd.RegisterFlagCompletionFunc(FlagSourceNamespace,
		buildKubeNSCompletionFunc(cmd.Context(), FlagSourceKubeconfig, FlagSourceContext))
	_ = cmd.RegisterFlagCompletionFunc(FlagSourcePath, completionFuncNoFileComplete)

	_ = cmd.RegisterFlagCompletionFunc(FlagDestContext, buildKubeContextCompletionFunc(FlagDestKubeconfig))
	_ = cmd.RegisterFlagCompletionFunc(FlagDestNamespace,
		buildKubeNSCompletionFunc(cmd.Context(), FlagDestKubeconfig, FlagDestContext))
	_ = cmd.RegisterFlagCompletionFunc(FlagDestPath, completionFuncNoFileComplete)

	_ = cmd.RegisterFlagCompletionFunc(FlagStrategies, buildSliceCompletionFunc(strategy.AllStrategies))
	_ = cmd.RegisterFlagCompletionFunc(FlagSSHKeyAlgorithm, buildStaticSliceCompletionFunc(ssh.KeyAlgorithms))

	_ = cmd.RegisterFlagCompletionFunc(FlagHelmSet, completionFuncNoFileComplete)
	_ = cmd.RegisterFlagCompletionFunc(FlagHelmSetString, completionFuncNoFileComplete)
	_ = cmd.RegisterFlagCompletionFunc(FlagHelmSetFile, completionFuncNoFileComplete)
}

func setMigrateCmdFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	flags.StringP(FlagSourceKubeconfig, "k", "", "path of the kubeconfig file of the source PVC")
	flags.StringP(FlagSourceContext, "c", "", "context in the kubeconfig file of the source PVC")
	flags.StringP(FlagSourceNamespace, "n", "", "namespace of the source PVC")
	flags.StringP(FlagSourcePath, "p", "/", "the filesystem path to migrate in the source PVC")

	flags.StringP(FlagDestKubeconfig, "K", "", "path of the kubeconfig file of the destination PVC")
	flags.StringP(FlagDestContext, "C", "", "context in the kubeconfig file of the destination PVC")
	flags.StringP(FlagDestNamespace, "N", "", "namespace of the destination PVC")
	flags.StringP(FlagDestPath, "P", "/", "the filesystem path to migrate in the destination PVC")

	flags.BoolP(FlagDestDeleteExtraneousFiles, "d", false,
		"delete extraneous files on the destination by using rsync's '--delete' flag")
	flags.BoolP(FlagIgnoreMounted, "i", false,
		"do not fail if the source or destination PVC is mounted")
	flags.BoolP(FlagNoChown, "o", false, "omit chown on rsync")
	flags.BoolP(FlagNoProgressBar, "b", false, "do not display a progress bar")
	flags.BoolP(FlagSourceMountReadOnly, "R", true, "mount the source PVC in ReadOnly mode")
	flags.StringSliceP(FlagStrategies, "s", strategy.DefaultStrategies,
		"the comma-separated list of strategies to be used in the given order")
	flags.StringP(FlagSSHKeyAlgorithm, "a", ssh.Ed25519KeyAlgorithm,
		fmt.Sprintf("ssh key algorithm to be used. Valid values are %s",
			strings.Join(ssh.KeyAlgorithms, ",")))
	flags.StringP(FlagDestHostOverride, "H", "",
		"the override for the rsync host destination when it is run over SSH, "+
			"in cases when you need to target a different destination IP on rsync for some reason. "+
			"By default, it is determined by used strategy and differs across strategies. "+
			"Has no effect for mnt2 and local strategies")
	flags.Duration(FlagLBSvcTimeout, lbSvcTimeoutDefault, fmt.Sprintf("timeout for the load balancer service to "+
		"receive an external IP. Only used by the %s strategy", strategy.LbSvcStrategy))

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

func runMigration(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()

	ignoreMounted, _ := flags.GetBool(FlagIgnoreMounted)
	srcMountReadOnly, _ := flags.GetBool(FlagSourceMountReadOnly)
	noChown, _ := flags.GetBool(FlagNoChown)
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

	deleteExtraneousFiles, _ := flags.GetBool(FlagDestDeleteExtraneousFiles)
	request := migration.Request{
		Source:                buildSrcPVCInfo(flags, args[0]),
		Dest:                  buildDestPVCInfo(flags, args[1]),
		DeleteExtraneousFiles: deleteExtraneousFiles,
		IgnoreMounted:         ignoreMounted,
		SourceMountReadOnly:   srcMountReadOnly,
		NoChown:               noChown,
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
		Logger:                logger,
	}

	logger.Info("🚀 Starting migration")

	if deleteExtraneousFiles {
		logger.Info("❕ Extraneous files will be deleted from the destination")
	}

	if err := migrator.New().Run(cmd.Context(), &request); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
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
