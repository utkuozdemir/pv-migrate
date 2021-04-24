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
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"sort"
	"strings"
)

// Engine is the main component that coordinates and runs the migration.
// It is responsible of processing the request, building a migration task, determine the execution order
// of the strategies and execute them until one of them succeeds.
type Engine interface {
	// Run runs the migration
	Run(request request.Request) error
	validate(request request.Request) error
	// BuildJob builds a Job from a Request
	BuildJob(request request.Request) (job.Job, error)
	determineStrategies(request request.Request, job job.Job) ([]strategy.Strategy, error)
	findStrategies(strategyNames ...string) ([]strategy.Strategy, error)
}

type engine struct {
	kubernetesClientProvider k8s.KubernetesClientProvider
	strategyMap              map[string]strategy.Strategy
}

// New creates a new engine with the given strategies
func New(strategies []strategy.Strategy) (Engine, error) {
	return NewWithKubernetesClientProvider(strategies, k8s.NewKubernetesClientProvider())
}

// NewWithKubernetesClientProvider creates a new engine with the given strategies and the kubernetes client provider
func NewWithKubernetesClientProvider(strategies []strategy.Strategy, kubernetesClientProvider k8s.KubernetesClientProvider) (Engine, error) {
	if len(strategies) == 0 {
		return nil, errors.New("no strategies passed")
	}

	strategyMap := make(map[string]strategy.Strategy)
	for _, s := range strategies {
		name := s.Name()
		if _, exists := strategyMap[name]; exists {
			return nil, errors.New("duplicate name in strategies")
		}
		strategyMap[name] = s
	}

	return &engine{
		kubernetesClientProvider: kubernetesClientProvider,
		strategyMap:              strategyMap}, nil
}

func (e *engine) Run(request request.Request) error {
	err := e.validate(request)
	if err != nil {
		return err
	}

	migrationJob, err := e.BuildJob(request)
	if err != nil {
		return err
	}

	strategies, err := e.determineStrategies(request, migrationJob)
	if err != nil {
		return err
	}

	logger := log.WithFields(request.LogFields())

	numStrategies := len(strategies)
	if numStrategies == 0 {
		return errors.New("no strategy found that can handle the request")
	}

	strategyNames := strategy.Names(strategies)
	logger.
		WithField("strategies", strings.Join(strategyNames, " ")).
		Infof("Determined %v strategies to be attempted", numStrategies)

	for _, s := range strategies {
		migrationTask := task.New(migrationJob)

		logger = log.WithFields(log.Fields{
			"strategy": s.Name(),
			"priority": s.Priority(),
		})

		logger.Info("Executing strategy")
		runErr := s.Run(migrationTask)
		if runErr != nil {
			logger.WithError(runErr).Warn("Migration failed, will try remaining strategies")
		} else {
			logger.Info("Migration succeeded")
		}

		logger.Info("Cleaning up")
		cleanupErr := s.Cleanup(migrationTask)
		if cleanupErr != nil {
			logger.WithError(cleanupErr).Warn("Cleanup failed, you might want to clean up manually")
		}

		if runErr == nil {
			return nil
		}
	}

	return errors.New("all strategies have failed")
}

func (e *engine) validate(request request.Request) error {
	for _, requestStrategy := range request.Strategies() {
		if _, exists := e.strategyMap[requestStrategy]; !exists {
			log.WithField("strategy", requestStrategy).Error("Requested strategy not found")
			return errors.New("requested strategy not found")
		}
	}

	return nil
}

func (e *engine) BuildJob(request request.Request) (job.Job, error) {
	id := util.RandomHexadecimalString(5)

	source := request.Source()
	dest := request.Dest()
	kubernetesClientProvider := e.kubernetesClientProvider
	var sourceClient, sourceNsInContext, err = kubernetesClientProvider.GetClientAndNsInContext(source.KubeconfigPath(), source.Context())
	if err != nil {
		return nil, err
	}

	destClient, destNsInContext := sourceClient, sourceNsInContext
	if source.KubeconfigPath() != dest.KubeconfigPath() || source.Context() != dest.Context() {
		destClient, destNsInContext, err = kubernetesClientProvider.GetClientAndNsInContext(dest.KubeconfigPath(), dest.Context())
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

	if !(destPvcInfo.SupportsRWO() || destPvcInfo.SupportsRWX()) {
		return nil, errors.New("destination pvc is not writeable")
	}

	taskOptions := job.NewOptions(request.Options().DeleteExtraneousFiles())
	return job.New(id, sourcePvcInfo, destPvcInfo, taskOptions), nil
}

func (e *engine) determineStrategies(request request.Request, job job.Job) ([]strategy.Strategy, error) {
	if len(request.Strategies()) > 0 {
		return e.findStrategies(request.Strategies()...)
	}

	var strategies []strategy.Strategy
	for _, s := range e.strategyMap {
		if (s).CanDo(job) {
			strategies = append(strategies, s)
		}
	}

	sort.Slice(strategies, func(i, j int) bool {
		return strategies[i].Priority() < strategies[j].Priority()
	})

	return strategies, nil
}

func (e *engine) findStrategies(strategyNames ...string) ([]strategy.Strategy, error) {
	var strategies []strategy.Strategy
	for _, strategyName := range strategyNames {
		s, exists := e.strategyMap[strategyName]
		if !exists {
			return nil, fmt.Errorf("strategy not found: %v", strategyName)
		}
		strategies = append(strategies, s)
	}

	return strategies, nil
}
