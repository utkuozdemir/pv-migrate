package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

var completionCmdLongDesc = fmt.Sprintf(`To load completions:

Bash:

  $ source <(%[1]s completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ %[1]s completion bash > /etc/bash_completion.d/%[1]s
  # macOS:
  $ %[1]s completion bash > /usr/local/etc/bash_completion.d/%[1]s

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ %[1]s completion zsh > "${fpath[1]}/_%[1]s"

  # You will need to start a new shell for this setup to take effect.

fish:

  $ %[1]s completion fish | source

  # To load completions for each session, execute once:
  $ %[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

PowerShell:

  PS> %[1]s completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> %[1]s completion powershell > %[1]s.ps1
  # and source this file from your PowerShell profile.
`, appName)

func buildCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:                   "completion [bash|zsh|fish|powershell]",
		Short:                 "Generate completion script",
		Long:                  completionCmdLongDesc,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error

			switch args[0] {
			case "bash":
				err = cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				err = cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				err = cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}

			if err != nil {
				return fmt.Errorf("failed to generate completion script: %w", err)
			}

			return nil
		},
	}
}

func completionLogger(cmd *cobra.Command) (*slog.Logger, error) {
	logLevel, err := cmd.Flags().GetString(FlagLogLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to get flag %q: %w", FlagLogLevel, err)
	}

	logFormat, err := cmd.Flags().GetString(FlagLogFormat)
	if err != nil {
		return nil, fmt.Errorf("failed to get flag %q: %w", FlagLogFormat, err)
	}

	logger, err := buildLogger(logLevel, logFormat, os.Stderr, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	return logger, nil
}

func buildKubeContextCompletionFunc(kubeconfigFlag string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		logger, err := completionLogger(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		srcKubeconfig, err := cmd.Flags().GetString(kubeconfigFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", kubeconfigFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		contexts, err := k8s.GetContexts(srcKubeconfig, logger)
		if err != nil {
			logger.Debug("failed to get contexts", "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return contexts, cobra.ShellCompDirectiveDefault
	}
}

func buildKubeNSCompletionFunc(ctx context.Context, kubeconfigFlag, contextFlag string) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		logger, err := completionLogger(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		srcKubeconfig, err := cmd.Flags().GetString(kubeconfigFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", kubeconfigFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		srcContext, err := cmd.Flags().GetString(contextFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", contextFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		contexts, err := k8s.GetNamespaces(ctx, srcKubeconfig, srcContext, logger)
		if err != nil {
			logger.Debug("failed to get namespaces", "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return contexts, cobra.ShellCompDirectiveDefault
	}
}

func buildStaticSliceCompletionFunc(values []string) cobra.CompletionFunc {
	return func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func buildSliceCompletionFunc(values []string) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		remaining := make(map[string]struct{}, len(values))
		for _, value := range values {
			remaining[value] = struct{}{}
		}

		provided := make([]string, 0, len(values))

		split := strings.SplitSeq(toComplete, ",")
		for s := range split {
			val := strings.TrimSpace(s)
			provided = append(provided, val)
			delete(remaining, val)
		}

		var suggestions []string

		lastPart := provided[len(provided)-1]
		for value := range remaining {
			if !strings.HasPrefix(value, lastPart) {
				continue
			}

			suffix := strings.TrimPrefix(value, lastPart)
			suggestions = append(suggestions, toComplete+suffix)
		}

		directive := cobra.ShellCompDirectiveNoFileComp
		if len(suggestions) > 1 {
			directive = cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
		}

		return suggestions, directive
	}
}

func buildPVCCompletionFunc(ctx context.Context, isDestPVC bool) cobra.CompletionFunc {
	kubeconfigFlag := FlagSourceKubeconfig
	contextFlag := FlagSourceContext
	namespaceFlag := FlagSourceNamespace

	if isDestPVC {
		kubeconfigFlag = FlagDestKubeconfig
		contextFlag = FlagDestContext
		namespaceFlag = FlagDestNamespace
	}

	return func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		logger, err := completionLogger(cmd)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		kubeconfig, err := cmd.Flags().GetString(kubeconfigFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", kubeconfigFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		useContext, err := cmd.Flags().GetString(contextFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", contextFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		namespace, err := cmd.Flags().GetString(namespaceFlag)
		if err != nil {
			logger.Debug("failed to get flag", "flag", namespaceFlag, "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		pvcs, err := k8s.GetPVCs(ctx, kubeconfig, useContext, namespace, logger)
		if err != nil {
			logger.Debug("failed to get PVCs", "error", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return pvcs, cobra.ShellCompDirectiveNoFileComp
	}
}
