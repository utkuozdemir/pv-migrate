package engine

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"strings"
)

// Engine is the main component that coordinates and runs the migration.
// It is responsible of processing the request, building a migration task, determine the execution order
// of the strategies and execute them until one of them succeeds.
type Engine interface {
	// Run runs the migration
	Run(request request.Request) error
	// BuildJob builds a Job from a Request
	BuildJob(request request.Request) (job.Job, error)
}

type engine struct {
	kubernetesClientProvider k8s.KubernetesClientProvider
}

// New creates a new engine
func New() Engine {
	return NewWithKubernetesClientProvider(k8s.NewKubernetesClientProvider())
}

// NewWithKubernetesClientProvider creates a new engine with the given kubernetes client provider
func NewWithKubernetesClientProvider(kubernetesClientProvider k8s.KubernetesClientProvider) Engine {
	return &engine{
		kubernetesClientProvider: kubernetesClientProvider,
	}
}

func (e *engine) Run(request request.Request) error {
	nameToStrategyMap, err := strategy.GetStrategiesMapForNames(request.Strategies())
	if err != nil {
		return err
	}

	migrationJob, err := e.BuildJob(request)
	if err != nil {
		return err
	}

	logger := log.WithFields(request.LogFields())
	logger.
		WithField("strategies", strings.Join(request.Strategies(), " ")).
		Infof("Will attempt %v strategies", len(nameToStrategyMap))

	for name, s := range nameToStrategyMap {
		migrationTask := task.New(migrationJob)

		logger = log.WithField("strategy", name)
		logger.Info("Attempting strategy")
		accepted, runErr := s.Run(migrationTask)
		if !accepted {
			logger.Info("Strategy cannot handle this request, will try the next one")
			continue
		}

		if runErr == nil {
			logger.Info("Migration succeeded")
			return nil
		}

		logger.WithError(runErr).
			Warn("Migration failed with this strategy, will try with the remaining strategies")
	}

	return errors.New("all strategies have failed")
}

func (e *engine) BuildJob(request request.Request) (job.Job, error) {
	source := request.Source()
	dest := request.Dest()
	kubernetesClientProvider := e.kubernetesClientProvider
	var sourceClient, sourceNsInContext, err = kubernetesClientProvider.
		GetClientAndNsInContext(source.KubeconfigPath(), source.Context())
	if err != nil {
		return nil, err
	}

	destClient, destNsInContext := sourceClient, sourceNsInContext
	if source.KubeconfigPath() != dest.KubeconfigPath() || source.Context() != dest.Context() {
		destClient, destNsInContext, err = kubernetesClientProvider.
			GetClientAndNsInContext(dest.KubeconfigPath(), dest.Context())
		if err != nil {
			return nil, err
		}
	}

	sourceNs := source.Namespace()
	if sourceNs == "" {
		sourceNs = sourceNsInContext
	}

	destNs := dest.Namespace()
	if destNs == "" {
		destNs = destNsInContext
	}

	sourcePvcInfo, err := pvc.New(sourceClient, sourceNs, source.Name())
	if err != nil {
		return nil, err
	}

	destPvcInfo, err := pvc.New(destClient, destNs, dest.Name())
	if err != nil {
		return nil, err
	}

	ignoreMounted := request.Options().IgnoreMounted()
	err = handleMounted(sourcePvcInfo, ignoreMounted)
	if err != nil {
		return nil, err
	}
	err = handleMounted(destPvcInfo, ignoreMounted)
	if err != nil {
		return nil, err
	}

	if !(destPvcInfo.SupportsRWO() || destPvcInfo.SupportsRWX()) {
		return nil, errors.New("destination pvc is not writeable")
	}

	taskOptions := job.NewOptions(request.Options().DeleteExtraneousFiles(), request.Options().NoChown())
	return job.New(sourcePvcInfo, destPvcInfo, taskOptions, request.RsyncImage(), request.SshdImage()), nil
}

func handleMounted(info pvc.Info, ignoreMounted bool) error {
	if info.MountedNode() == "" {
		return nil
	}

	if ignoreMounted {
		log.Infof("PVC %s is mounted to node %s, ignoring...", info.Claim().Name, info.MountedNode())
		return nil
	}
	return fmt.Errorf("PVC %s is mounted to node %s and ignore-mounted is not requested",
		info.Claim().Name, info.MountedNode())
}
