package strategy

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/migration"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/storage/driver"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	Mnt2Strategy  = "mnt2"
	SvcStrategy   = "svc"
	LbSvcStrategy = "lbsvc"
	LocalStrategy = "local"
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
)

type Strategy interface {
	// Run runs the migration for the given task execution.
	//
	// This is the actual implementation of the migration.
	Run(a *migration.Attempt) (bool, error)
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

func registerCleanupHook(a *migration.Attempt, releaseNames []string) chan<- bool {
	doneCh := make(chan bool)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalCh:
			a.Logger.Warn(":large_orange_diamond: Received termination signal")
			cleanup(a, releaseNames)
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
	logger := a.Logger
	logger.Info(":broom: Cleaning up")
	var result *multierror.Error

	for _, info := range []*pvc.Info{mig.SourceInfo, mig.DestInfo} {
		for _, name := range releaseNames {
			err := cleanupForPVC(logger, name, info)
			if err != nil {
				result = multierror.Append(result, err)
			}
		}
	}

	err := result.ErrorOrNil()
	if err != nil {
		logger.WithError(err).
			Warn(":large_orange_diamond: Cleanup failed, you might want to clean up manually")
		return
	}

	logger.Info(":sparkles: Cleanup done")
}

func cleanupForPVC(logger *log.Entry, helmReleaseName string, pvcInfo *pvc.Info) error {
	ac, err := initHelmActionConfig(logger, pvcInfo)
	if err != nil {
		return err
	}

	uninstall := action.NewUninstall(ac)
	uninstall.Wait = true
	uninstall.Timeout = 1 * time.Minute
	_, err = uninstall.Run(helmReleaseName)

	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func initHelmActionConfig(logger *log.Entry, pvcInfo *pvc.Info) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)
	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"), logger.Debugf)
	if err != nil {
		return nil, err
	}
	return actionConfig, nil
}

func getMergedHelmValues(helmValuesFile string, r *migration.Request) (map[string]interface{}, error) {
	allValuesFiles := append([]string{helmValuesFile}, r.HelmValuesFiles...)
	valsOptions := values.Options{
		Values:       r.HelmValues,
		ValueFiles:   allValuesFiles,
		StringValues: r.HelmStringValues,
		FileValues:   r.HelmFileValues,
	}

	return valsOptions.MergeValues(helmProviders)
}

func installHelmChart(a *migration.Attempt, pvcInfo *pvc.Info, name string,
	values map[string]interface{},
) error {
	helmValuesFile, err := writeHelmValuesToTempFile(a.ID, values)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(helmValuesFile) }()

	helmActionConfig, err := initHelmActionConfig(a.Logger, pvcInfo)
	if err != nil {
		return err
	}

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	install.Wait = true
	install.Timeout = 1 * time.Minute

	mig := a.Migration
	vals, err := getMergedHelmValues(helmValuesFile, mig.Request)
	if err != nil {
		return err
	}

	_, err = install.Run(mig.Chart, vals)
	return err
}

func writeHelmValuesToTempFile(id string, vals map[string]interface{}) (string, error) {
	f, err := ioutil.TempFile("", fmt.Sprintf("pv-migrate-vals-%s-*.yaml", id))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	encoder := yaml.NewEncoder(f)
	encoder.SetIndent(2)
	err = encoder.Encode(vals)
	if err != nil {
		return "", err
	}

	return f.Name(), nil
}
