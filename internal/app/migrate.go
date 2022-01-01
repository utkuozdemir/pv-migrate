package app

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/utkuozdemir/pv-migrate/engine"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/migration"
	"strings"
)

const (
	CommandMigrate = "migrate"

	FlagSourceKubeconfig = "source-kubeconfig"
	FlagSourceContext    = "source-context"
	FlagSourceNamespace  = "source-namespace"
	FlagSourcePath       = "source-path"

	FlagDestKubeconfig = "dest-kubeconfig"
	FlagDestContext    = "dest-context"
	FlagDestNamespace  = "dest-namespace"
	FlagDestPath       = "dest-path"

	FlagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	FlagIgnoreMounted             = "ignore-mounted"
	FlagNoChown                   = "no-chown"
	FlagNoProgressBar             = "no-progress-bar"
	FlagSourceMountReadOnly       = "source-mount-read-only"
	FlagStrategies                = "strategies"
	FlagSSHKeyAlgorithm           = "ssh-key-algorithm"

	FlagHelmValues    = "helm-values"
	FlagHelmSet       = "helm-set"
	FlagHelmSetString = "helm-set-string"
	FlagHelmSetFile   = "helm-set-file"
)

func buildMigrateCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:     CommandMigrate,
		Aliases: []string{"m"},
		Short:   "Migrate data from one Kubernetes PersistentVolumeClaim to another",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()

			srcKubeconfigPath, _ := f.GetString(FlagSourceKubeconfig)
			srcContext, _ := f.GetString(FlagSourceContext)
			srcNS, _ := f.GetString(FlagSourceNamespace)
			srcPath, _ := f.GetString(FlagSourcePath)

			destKubeconfigPath, _ := f.GetString(FlagDestKubeconfig)
			destContext, _ := f.GetString(FlagDestContext)
			destNS, _ := f.GetString(FlagDestNamespace)
			destPath, _ := f.GetString(FlagDestPath)

			ignoreMounted, _ := f.GetBool(FlagIgnoreMounted)
			srcMountReadOnly, _ := f.GetBool(FlagSourceMountReadOnly)
			noChown, _ := f.GetBool(FlagNoChown)
			noProgressBar, _ := f.GetBool(FlagNoProgressBar)
			sshKeyAlg, _ := f.GetString(FlagSSHKeyAlgorithm)
			helmValues, _ := f.GetStringSlice(FlagHelmValues)
			helmSet, _ := f.GetStringSlice(FlagHelmSet)
			helmSetString, _ := f.GetStringSlice(FlagHelmSetString)
			helmSetFile, _ := f.GetStringSlice(FlagHelmSetFile)
			strs, _ := f.GetStringSlice(FlagStrategies)

			deleteExtraneousFiles, _ := f.GetBool(FlagDestDeleteExtraneousFiles)
			m := migration.Request{
				Source: &migration.PVCInfo{
					KubeconfigPath: srcKubeconfigPath,
					Context:        srcContext,
					Namespace:      srcNS,
					Name:           args[0],
					Path:           srcPath,
				},
				Dest: &migration.PVCInfo{
					KubeconfigPath: destKubeconfigPath,
					Context:        destContext,
					Namespace:      destNS,
					Name:           args[1],
					Path:           destPath,
				},
				DeleteExtraneousFiles: deleteExtraneousFiles,
				IgnoreMounted:         ignoreMounted,
				SourceMountReadOnly:   srcMountReadOnly,
				NoChown:               noChown,
				NoProgressBar:         noProgressBar,
				KeyAlgorithm:          sshKeyAlg,
				HelmValuesFiles:       helmValues,
				HelmValues:            helmSet,
				HelmStringValues:      helmSetString,
				HelmFileValues:        helmSetFile,
				Strategies:            strs,
				Logger:                logger,
			}

			logger.Info(":rocket: Starting migration")
			if deleteExtraneousFiles {
				logger.Info(":white_exclamation_mark: " +
					"Extraneous files will be deleted from the destination")
			}

			return engine.New().Run(&m)

		},
	}

	f := cmd.Flags()

	f.StringP(FlagSourceKubeconfig, "k", "", "path of the kubeconfig file of the source PVC")
	f.StringP(FlagSourceContext, "c", "", "context in the kubeconfig file of the source PVC")
	f.StringP(FlagSourceNamespace, "n", "", "namespace of the source PVC")
	f.StringP(FlagSourcePath, "p", "/", "the filesystem path to migrate in the the source PVC")

	f.StringP(FlagDestKubeconfig, "K", "", "path of the kubeconfig file of the destination PVC")
	f.StringP(FlagDestContext, "C", "", "context in the kubeconfig file of the destination PVC")
	f.StringP(FlagDestNamespace, "N", "", "namespace of the destination PVC")
	f.StringP(FlagDestPath, "P", "/", "the filesystem path to migrate in the the destination PVC")

	f.BoolP(FlagDestDeleteExtraneousFiles, "d", false, "delete extraneous files on the destination by using rsync's '--delete' flag")
	f.BoolP(FlagIgnoreMounted, "i", false, "do not fail if the source or destination PVC is mounted")
	f.BoolP(FlagNoChown, "o", false, "omit chown on rsync")
	f.BoolP(FlagNoProgressBar, "b", false, "do not display a progress bar")
	f.BoolP(FlagSourceMountReadOnly, "R", true, "mount the source PVC in ReadOnly mode")
	f.StringSliceP(FlagStrategies, "s", strategy.DefaultStrategies, "the comma-separated list of strategies to be used in the given order")
	f.StringP(FlagSSHKeyAlgorithm, "a", ssh.Ed25519KeyAlgorithm, fmt.Sprintf("ssh key algorithm to be used. Valid values are %s", strings.Join(ssh.KeyAlgorithms, ",")))

	f.StringSliceP(FlagHelmValues, "f", nil, "set additional Helm values by a YAML file or a URL (can specify multiple)")
	f.StringSlice(FlagHelmSet, nil, "set additional Helm values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringSlice(FlagHelmSetString, nil, "set additional Helm STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringSlice(FlagHelmSetFile, nil, "set additional Helm values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")

	return &cmd
}
