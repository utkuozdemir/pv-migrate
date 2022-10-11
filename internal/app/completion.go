package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

var completionCmdlongDesc = fmt.Sprintf(`To load completions:

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
		Long:                  completionCmdlongDesc,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}

			return nil
		},
	}
}

func buildKubeContextCompletionFunc(kubeconfigFlag string) func(cmd *cobra.Command,
	args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		srcKubeconfig, _ := cmd.Flags().GetString(kubeconfigFlag)

		contexts, err := k8s.GetContexts(srcKubeconfig)
		if err != nil {
			logger.Tracef("error: %v", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return contexts, cobra.ShellCompDirectiveDefault
	}
}

func buildKubeNSCompletionFunc(kubeconfigFlag string, contextFlag string) func(cmd *cobra.Command,
	args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		srcKubeconfig, _ := cmd.Flags().GetString(kubeconfigFlag)
		srcContext, _ := cmd.Flags().GetString(contextFlag)

		contexts, err := k8s.GetNamespaces(srcKubeconfig, srcContext)
		if err != nil {
			logger.Tracef("error: %v", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return contexts, cobra.ShellCompDirectiveDefault
	}
}

func buildStaticSliceCompletionFunc(values []string) func(cmd *cobra.Command,
	args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func buildSliceCompletionFunc(values []string) func(cmd *cobra.Command,
	args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		remaining := make(map[string]struct{}, len(values))
		for _, value := range values {
			remaining[value] = struct{}{}
		}

		var provided []string

		split := strings.Split(toComplete, ",")
		for _, s := range split {
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

func buildPVCsCompletionFunc() func(cmd *cobra.Command, args []string,
	toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= migrateCmdNumArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		kubeconfig, _ := cmd.Flags().GetString(FlagSourceKubeconfig)
		context, _ := cmd.Flags().GetString(FlagSourceContext)
		namespace, _ := cmd.Flags().GetString(FlagSourceNamespace)

		if len(args) == 1 {
			kubeconfig, _ = cmd.Flags().GetString(FlagDestKubeconfig)
			context, _ = cmd.Flags().GetString(FlagDestContext)
			namespace, _ = cmd.Flags().GetString(FlagDestNamespace)
		}

		pvcs, err := k8s.GetPVCs(kubeconfig, context, namespace)
		if err != nil {
			logger.Tracef("error: %v", err)

			return nil, cobra.ShellCompDirectiveError
		}

		return pvcs, cobra.ShellCompDirectiveNoFileComp
	}
}
