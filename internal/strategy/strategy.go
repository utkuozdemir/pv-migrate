package strategy

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"os"
	"os/signal"
	"syscall"
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
			e.Logger.Warn(":warn: Received termination signal")
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
	err := k8s.CleanupForID(s.KubeClient, s.Claim.Namespace, e.ID)
	if err != nil {
		result = multierror.Append(result, err)
	}
	d := t.DestInfo
	err = k8s.CleanupForID(d.KubeClient, d.Claim.Namespace, e.ID)
	if err != nil {
		result = multierror.Append(result, err)
	}

	//goland:noinspection GoNilness
	err = result.ErrorOrNil()
	if err != nil {
		logger.WithError(err).
			Warn(":warn: Cleanup failed, you might want to clean up manually")
		return
	}

	logger.Info(":sparkles: Cleanup successful")
}
