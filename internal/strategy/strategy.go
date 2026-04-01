package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hashicorp/go-multierror"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

const (
	mountStrategy        = "mount"
	clusterIPStrategy    = "clusterip"
	loadBalancerStrategy = "loadbalancer"
	localStrategy        = "local"
	nodePortStrategy     = "nodeport"

	srcMountPath  = "/source"
	destMountPath = "/dest"

	rootSSHUser    = "root"
	rootSSHPort    = 22
	nonRootSSHUser = "pvmigrate"
	nonRootSSHPort = 2222
	nonRootUID     = 10000
)

var (
	nameToStrategy = map[string]Strategy{
		mountStrategy:        &Mount{},
		clusterIPStrategy:    &ClusterIP{},
		loadBalancerStrategy: &LoadBalancer{},
		localStrategy:        &Local{},
		nodePortStrategy:     &NodePort{},
	}

	helmProviders = getter.All(cli.New())

	ErrUnaccepted = errors.New("unaccepted")
)

type Strategy interface {
	// Run runs the migration for the given task execution.
	//
	// This is the actual implementation of the migration.
	Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error
}

func GetStrategiesMapForNames(names []string) (map[string]Strategy, error) {
	sts := make(map[string]Strategy)

	for _, name := range names {
		s, ok := nameToStrategy[name]
		if !ok {
			return nil, fmt.Errorf("strategy not found: %s", name)
		}

		sts[name] = s
	}

	return sts, nil
}

func Cleanup(attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	req := mig.Request

	logger.Info("🧹 Cleaning up")

	var errs error

	for _, info := range []*pvc.Info{mig.SourceInfo, mig.DestInfo} {
		for _, name := range attempt.ReleaseNames {
			err := cleanupForPVC(name, req.HelmTimeout, info)
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}
	}

	return errs
}

func cleanupForPVC(helmReleaseName string, helmUninstallTimeout time.Duration, pvcInfo *pvc.Info) error {
	ac, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return err
	}

	uninstall := action.NewUninstall(ac)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = helmUninstallTimeout
	_, err = uninstall.Run(helmReleaseName)

	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to uninstall helm release %s: %w", helmReleaseName, err)
	}

	return nil
}

func initHelmActionConfig(pvcInfo *pvc.Info) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	return actionConfig, nil
}

func sshUser(req *migration.Request) string {
	if req.NonRoot {
		return nonRootSSHUser
	}

	return rootSSHUser
}

func sshPort(req *migration.Request) int {
	if req.NonRoot {
		return nonRootSSHPort
	}

	return rootSSHPort
}

func applyNonRootValues(vals map[string]any, req *migration.Request) {
	if !req.NonRoot {
		return
	}

	nonRootSecCtx := map[string]any{
		"runAsNonRoot":             true,
		"runAsUser":                nonRootUID,
		"runAsGroup":               nonRootUID,
		"allowPrivilegeEscalation": false,
	}
	nonRootPodSecCtx := map[string]any{
		"fsGroup": nonRootUID,
	}

	for _, component := range []string{"sshd", "rsync"} {
		section, ok := vals[component].(map[string]any)
		if !ok {
			continue
		}

		section["securityContext"] = nonRootSecCtx
		section["podSecurityContext"] = nonRootPodSecCtx
	}

	if sshd, ok := vals["sshd"].(map[string]any); ok {
		sshd["containerPort"] = nonRootSSHPort
		sshd["publicKeyMountPath"] = "/home/pvmigrate/.ssh/authorized_keys"
	}
}

func getMergedHelmValues(
	baseValues map[string]any,
	request *migration.Request,
	logger *slog.Logger,
) (map[string]any, error) {
	// If an image tag is set, inject it as the lowest-priority --set values
	// so user overrides via --helm-set take precedence.
	helmValues := request.HelmValues
	if tag := request.ImageTag; tag != "" {
		imageTagValues := []string{
			"rsync.image.tag=" + tag,
			"sshd.image.tag=" + tag,
		}
		merged := make([]string, 0, len(imageTagValues)+len(helmValues))
		merged = append(merged, imageTagValues...)
		helmValues = append(merged, helmValues...)
	}

	valsOptions := values.Options{
		ValueFiles:   request.HelmValuesFiles,
		Values:       helmValues,
		StringValues: request.HelmStringValues,
		FileValues:   request.HelmFileValues,
	}

	userValues, err := valsOptions.MergeValues(helmProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to merge helm values: %w", err)
	}

	// Merge using Helm's own MergeMaps: user values override base values.
	merged := loader.MergeMaps(baseValues, userValues)

	if request.ImageTag != "" {
		logger.Info("🏷️ Using image tag", "tag", request.ImageTag)
	} else {
		logger.Info("🏷️ Using chart default image tags")
	}

	return merged, nil
}

func installHelmChart(
	attempt *migration.Attempt,
	pvcInfo *pvc.Info,
	name string,
	values map[string]any,
	logger *slog.Logger,
) error {
	helmActionConfig, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	mig := attempt.Migration

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	install.WaitStrategy = kube.LegacyStrategy

	if req := mig.Request; req.HelmTimeout < req.LoadBalancerTimeout {
		install.Timeout = req.LoadBalancerTimeout
	} else {
		install.Timeout = req.HelmTimeout
	}

	applyNonRootValues(values, mig.Request)

	vals, err := getMergedHelmValues(values, mig.Request, logger)
	if err != nil {
		return fmt.Errorf("failed to get merged helm values: %w", err)
	}

	if _, err = install.Run(mig.Chart, vals); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return nil
}
