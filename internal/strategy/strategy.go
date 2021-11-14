package strategy

import (
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/storage/driver"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	Mnt2Strategy  = "mnt2"
	SvcStrategy   = "svc"
	LbSvcStrategy = "lbsvc"
)

var (
	DefaultStrategies = []string{Mnt2Strategy, SvcStrategy, LbSvcStrategy}

	nameToStrategy = map[string]Strategy{
		Mnt2Strategy:  &Mnt2{},
		SvcStrategy:   &Svc{},
		LbSvcStrategy: &LbSvc{},
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

func registerCleanupHook(e *task.Execution) chan<- bool {
	doneCh := make(chan bool)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalCh:
			e.Logger.Warn(":large_orange_diamond: Received termination signal")
			cleanup(e)
			os.Exit(1)
		case <-doneCh:
			return
		}
	}()
	return doneCh
}

func cleanupAndReleaseHook(e *task.Execution, doneCh chan<- bool) {
	cleanup(e)
	doneCh <- true
}

func cleanup(e *task.Execution) {
	t := e.Task
	logger := e.Logger
	logger.Info(":broom: Cleaning up")
	var result *multierror.Error
	s := t.SourceInfo

	err := cleanupForPVC(logger, e.HelmReleaseName, s)
	if err != nil {
		result = multierror.Append(result, err)
	}

	d := t.DestInfo
	err = cleanupForPVC(logger, e.HelmReleaseName, d)
	if err != nil {
		result = multierror.Append(result, err)

	}

	err = result.ErrorOrNil()
	if err != nil {
		logger.WithError(err).
			Warn(":large_orange_diamond: Cleanup failed, you might want to clean up manually")
		return
	}

	logger.Info(":sparkles: Cleanup done")
}

func cleanupForPVC(logger *log.Entry, helmReleaseName string, pvcInfo *pvc.Info) error {
	sourceHelmActionConfig, err := initHelmActionConfig(logger, pvcInfo)
	if err != nil {
		return err
	}

	uninstall := action.NewUninstall(sourceHelmActionConfig)
	uninstall.Wait = true
	uninstall.Timeout = 1 * time.Minute
	_, err = uninstall.Run(helmReleaseName)

	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
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

func getMergedHelmValues(helmValues []string) (map[string]interface{}, error) {
	valsOptions := values.Options{
		Values: helmValues,
	}

	return valsOptions.MergeValues(helmProviders)
}
