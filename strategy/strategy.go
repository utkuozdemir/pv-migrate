package strategy

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/pvc"
)

const (
	Mnt2Strategy  = "mnt2"
	SvcStrategy   = "svc"
	LbSvcStrategy = "lbsvc"
	LocalStrategy = "local"

	helmValuesYAMLIndent = 2

	srcMountPath  = "/source"
	destMountPath = "/dest"
)

var (
	DefaultStrategies = []string{Mnt2Strategy, SvcStrategy, LbSvcStrategy}
	AllStrategies     = []string{Mnt2Strategy, SvcStrategy, LbSvcStrategy, LocalStrategy}

	nameToStrategy = map[string]Strategy{
		Mnt2Strategy:  &Mnt2{},
		SvcStrategy:   &Svc{},
		LbSvcStrategy: &LbSvc{},
		LocalStrategy: &Local{},
	}

	helmProviders = getter.All(cli.New())

	ErrUnaccepted = errors.New("unaccepted")
)

type Strategy interface {
	// Run runs the migration for the given task execution.
	//
	// This is the actual implementation of the migration.
	Run(a *migration.Attempt) error
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

func registerCleanupHook(attempt *migration.Attempt, releaseNames []string) chan<- bool {
	doneCh := make(chan bool)
	signalCh := make(chan os.Signal, 1)

	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-signalCh:
			attempt.Logger.Warn(":large_orange_diamond: Received termination signal")
			cleanup(attempt, releaseNames)
			os.Exit(1)
		case <-doneCh:
			return
		}
	}()

	return doneCh
}

func cleanupAndReleaseHook(a *migration.Attempt, releaseNames []string, doneCh chan<- bool) {
	cleanup(a, releaseNames)
	doneCh <- true
}

func cleanup(a *migration.Attempt, releaseNames []string) {
	mig := a.Migration
	req := mig.Request
	logger := a.Logger
	logger.Info(":broom: Cleaning up")

	var result *multierror.Error

	for _, info := range []*pvc.Info{mig.SourceInfo, mig.DestInfo} {
		for _, name := range releaseNames {
			err := cleanupForPVC(logger, name, req.HelmTimeout, info)
			if err != nil {
				result = multierror.Append(result, err)
			}
		}
	}

	if err := result.ErrorOrNil(); err != nil {
		logger.WithError(err).
			Warn(":large_orange_diamond: Cleanup failed, you might want to clean up manually")

		return
	}

	logger.Info(":sparkles: Cleanup done")
}

func cleanupForPVC(logger *log.Entry, helmReleaseName string,
	helmUninstallTimeout time.Duration, pvcInfo *pvc.Info,
) error {
	ac, err := initHelmActionConfig(logger, pvcInfo)
	if err != nil {
		return err
	}

	uninstall := action.NewUninstall(ac)
	uninstall.Wait = true
	uninstall.Timeout = helmUninstallTimeout
	_, err = uninstall.Run(helmReleaseName)

	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to uninstall helm release %s: %w", helmReleaseName, err)
	}

	return nil
}

func initHelmActionConfig(logger *log.Entry, pvcInfo *pvc.Info) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"), logger.Debugf)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	return actionConfig, nil
}

func getMergedHelmValues(helmValuesFile string, request *migration.Request) (map[string]any, error) {
	allValuesFiles := append([]string{helmValuesFile}, request.HelmValuesFiles...)
	valsOptions := values.Options{
		Values:       request.HelmValues,
		ValueFiles:   allValuesFiles,
		StringValues: request.HelmStringValues,
		FileValues:   request.HelmFileValues,
	}

	mergedValues, err := valsOptions.MergeValues(helmProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to merge helm values: %w", err)
	}

	return mergedValues, nil
}

func installHelmChart(attempt *migration.Attempt, pvcInfo *pvc.Info, name string,
	values map[string]any,
) error {
	helmValuesFile, err := writeHelmValuesToTempFile(attempt.ID, values)
	if err != nil {
		return fmt.Errorf("failed to write helm values to temp file: %w", err)
	}

	defer func() { _ = os.Remove(helmValuesFile) }()

	helmActionConfig, err := initHelmActionConfig(attempt.Logger, pvcInfo)
	if err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	mig := attempt.Migration
	req := mig.Request

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	install.Wait = true
	install.Timeout = req.HelmTimeout

	vals, err := getMergedHelmValues(helmValuesFile, mig.Request)
	if err != nil {
		return fmt.Errorf("failed to get merged helm values: %w", err)
	}

	if _, err = install.Run(mig.Chart, vals); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return nil
}

func writeHelmValuesToTempFile(id string, vals map[string]any) (string, error) {
	file, err := os.CreateTemp("", fmt.Sprintf("pv-migrate-vals-%s-*.yaml", id))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for helm values: %w", err)
	}

	defer func() { _ = file.Close() }()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(helmValuesYAMLIndent)

	err = encoder.Encode(vals)
	if err != nil {
		return "", fmt.Errorf("failed to encode helm values: %w", err)
	}

	return file.Name(), nil
}
