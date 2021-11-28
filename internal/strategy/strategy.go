package strategy

import (
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/migration"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/storage/driver"
	"io/ioutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	Mnt2Strategy  = "mnt2"
	SvcStrategy   = "svc"
	LbSvcStrategy = "lbsvc"
	LocalStrategy = "local"
)

var (
	DefaultStrategies = []string{Mnt2Strategy, SvcStrategy, LbSvcStrategy, LocalStrategy}

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
	Run(execution *task.Execution) (bool, error)
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

func registerCleanupHook(e *task.Execution, releaseNames []string) chan<- bool {
	doneCh := make(chan bool)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalCh:
			e.Logger.Warn(":large_orange_diamond: Received termination signal")
			cleanup(e, releaseNames)
			os.Exit(1)
		case <-doneCh:
			return
		}
	}()
	return doneCh
}

func cleanupAndReleaseHook(e *task.Execution, releaseNames []string, doneCh chan<- bool) {
	cleanup(e, releaseNames)
	doneCh <- true
}

func cleanup(e *task.Execution, releaseNames []string) {
	t := e.Task
	logger := e.Logger
	logger.Info(":broom: Cleaning up")
	var result *multierror.Error

	for _, info := range []*pvc.Info{t.SourceInfo, t.DestInfo} {
		for _, name := range releaseNames {
			err := cleanupForPVC(logger, name, info)
			if err != nil {
				result = multierror.Append(result, err)
			}
		}
	}

	err := result.ErrorOrNil()
	if err != nil {
		fmt.Println(err.Error())
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

func getMergedHelmValues(helmValuesFile string, opts *migration.Options) (map[string]interface{}, error) {
	allValuesFiles := append([]string{helmValuesFile}, opts.HelmValuesFiles...)
	valsOptions := values.Options{
		Values:       opts.HelmValues,
		ValueFiles:   allValuesFiles,
		StringValues: opts.HelmStringValues,
		FileValues:   opts.HelmFileValues,
	}

	return valsOptions.MergeValues(helmProviders)
}

func installHelmChart(e *task.Execution, pvcInfo *pvc.Info, name string,
	values map[string]interface{}) error {
	helmValuesFile, err := writeHelmValuesToTempFile(e.ID, values)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(helmValuesFile) }()

	helmActionConfig, err := initHelmActionConfig(e.Logger, pvcInfo)
	if err != nil {
		return err
	}

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	install.Wait = true
	install.Timeout = 1 * time.Minute

	t := e.Task
	vals, err := getMergedHelmValues(helmValuesFile, t.Migration.Options)
	if err != nil {
		return err
	}

	_, err = install.Run(t.Chart, vals)
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
