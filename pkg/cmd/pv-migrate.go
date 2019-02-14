package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"strings"
)


// NewCmdNamespace provides a cobra command wrapping NamespaceOptions
func NewCmdPvMigrate(streams genericclioptions.IOStreams) *cobra.Command {

	cmd := &cobra.Command{
		Use:   "print [string to print]",
		Short: "Print anything to the screen",
		Long: `print is for printing anything back to the screen.
               For many years people have printed back to the screen.`,
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Print: " + strings.Join(args, " "))
		},
	}

	//o := NewNamespaceOptions(streams)

	//cmd := &cobra.Command{
	//	Use:          "ns [new-namespace] [flags]",
	//	Short:        "View or set the current namespace",
	//	Example:      fmt.Sprintf(namespaceExample, "kubectl"),
	//	SilenceUsage: true,
	//	RunE: func(c *cobra.Command, args []string) error {
	//		if err := o.Complete(c, args); err != nil {
	//			return err
	//		}
	//		if err := o.Validate(); err != nil {
	//			return err
	//		}
	//		if err := o.Run(); err != nil {
	//			return err
	//		}
	//
	//		return nil
	//	},
	//}

	//cmd.Flags().BoolVar(&o.listNamespaces, "list", o.listNamespaces, "if true, print the list of all namespaces in the current KUBECONFIG")
	//o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
