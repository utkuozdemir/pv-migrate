package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/spf13/cobra"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

const helmReleasePrefix = "pv-migrate-"

func buildCleanupCmd(logger **slog.Logger) *cobra.Command {
	var (
		kubeconfig  string
		kubeContext string
		namespace   string
		all         bool
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup [operation-id]",
		Short: "Clean up resources from a detached operation",
		Long: "Remove Helm releases created by a detached operation. " +
			"Provide the operation ID printed by --detach, or use --all to remove all pv-migrate releases.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				return errors.New("provide an operation ID or use --all")
			}

			var filterPrefix string
			if all {
				filterPrefix = helmReleasePrefix
			} else {
				if args[0] == "" {
					return errors.New("operation ID must not be empty")
				}

				filterPrefix = helmReleasePrefix + args[0] + "-"
			}

			return runCleanup(
				cmd.Context(),
				*logger,
				kubeconfig,
				kubeContext,
				namespace,
				filterPrefix,
				all,
				force,
			)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	flags.StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	flags.StringVarP(&namespace, "namespace", "n", "", "Namespace to search for releases (default: all namespaces)")
	flags.BoolVar(&all, "all", false, "Remove all pv-migrate releases")
	flags.BoolVar(&force, "force", false, "Clean up even if the operation is still running")

	return cmd
}

func runCleanup(
	ctx context.Context,
	logger *slog.Logger,
	kubeconfig, kubeContext, namespace, filterPrefix string,
	all, force bool,
) error {
	client, err := k8s.GetClusterClient(kubeconfig, kubeContext, logger)
	if err != nil {
		return err
	}

	searchNs := namespace
	allNamespaces := namespace == ""

	ac := new(action.Configuration)
	if err := ac.Init(client.RESTClientGetter, searchNs, os.Getenv("HELM_DRIVER")); err != nil {
		return fmt.Errorf("failed to initialize helm: %w", err)
	}

	list := action.NewList(ac)
	list.Filter = "^" + regexp.QuoteMeta(filterPrefix)
	list.AllNamespaces = allNamespaces
	list.StateMask = action.ListAll

	releases, err := list.Run()
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	if len(releases) == 0 {
		if all {
			logger.Info("No pv-migrate releases found")

			return nil
		}

		return fmt.Errorf("no releases found matching %q", filterPrefix)
	}

	logger.Info("Found releases to clean up", "count", len(releases))

	if !force {
		if err = checkNoActiveJobs(ctx, client.KubeClient, releases, logger); err != nil {
			return err
		}
	}

	return uninstallReleases(releases, client, logger)
}

func checkNoActiveJobs(
	ctx context.Context, cli kubernetes.Interface, releases []release.Releaser, logger *slog.Logger,
) error {
	for _, rel := range releases {
		acc, err := release.NewAccessor(rel)
		if err != nil {
			continue
		}

		jobs, err := cli.BatchV1().Jobs(acc.Namespace()).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/instance=" + acc.Name(),
		})
		if err != nil {
			return fmt.Errorf("failed to list jobs for release %s: %w", acc.Name(), err)
		}

		for i := range jobs.Items {
			if jobs.Items[i].Status.Active > 0 {
				return fmt.Errorf("operation job %s/%s is still running; use --force to clean up anyway",
					jobs.Items[i].Namespace, jobs.Items[i].Name)
			}
		}
	}

	logger.Info("No active jobs found, proceeding with cleanup")

	return nil
}

func uninstallReleases(releases []release.Releaser, client *k8s.ClusterClient, logger *slog.Logger) error {
	for _, rel := range releases {
		acc, err := release.NewAccessor(rel)
		if err != nil {
			continue
		}

		if err = uninstallRelease(acc.Name(), acc.Namespace(), client); err != nil {
			return err
		}

		logger.Info("Uninstalled release", "release", acc.Name(), "namespace", acc.Namespace())
	}

	return nil
}

func uninstallRelease(name, namespace string, client *k8s.ClusterClient) error {
	ac := new(action.Configuration)
	if err := ac.Init(client.RESTClientGetter, namespace, os.Getenv("HELM_DRIVER")); err != nil {
		return fmt.Errorf("failed to initialize helm for namespace %s: %w", namespace, err)
	}

	uninstall := action.NewUninstall(ac)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = 1 * time.Minute

	if _, err := uninstall.Run(name); err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("failed to uninstall release %s: %w", name, err)
	}

	return nil
}
