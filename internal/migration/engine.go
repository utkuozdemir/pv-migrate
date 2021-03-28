package migration

import (
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"k8s.io/client-go/kubernetes"
	"sort"
)

type Engine interface {
	Run(request *Request)
}

type engine struct {
	strategyMap map[string]*Strategy
}

func NewEngine(strategies []Strategy) (*engine, error) {
	strategyMap := make(map[string]*Strategy)
	for _, strategy := range strategies {
		name := strategy.Name()

		if _, exists := strategyMap[name]; exists {
			return nil, errors.New("duplicate name in strategies")
		}

		strategyMap[name] = &strategy
	}

	return &engine{strategyMap: strategyMap}, nil
}

func (e *engine) Run(request *Request) error {
	err := e.validate(request)
	if err != nil {
		return err
	}

	task, err := e.buildTask(request)
	if err != nil {
		return err
	}

	strategies := e.determineStrategies(request, task)
	logger := log.WithFields(request.LogFields())

	if len(strategies) == 0 {
		return errors.New("no strategy found that can handle the request")
	}

	for _, strategy := range strategies {
		s := *strategy
		logger = log.WithFields(log.Fields{
			"strategy": s.Name(),
			"priority": s.Priority(),
		})

		logger.Info("Executing strategy")
		runErr := s.Run(task)
		if runErr != nil {
			logger.WithError(runErr).Warn("Migration failed, will try remaining strategies")
		} else {
			logger.Info("Migration succeeded")
		}

		logger.Info("Cleaning up")
		cleanupErr := s.Cleanup(task)
		if cleanupErr != nil {
			logger.WithError(cleanupErr).Warn("Cleanup failed, you might want to clean up manually")
		}

		if runErr == nil {
			return nil
		}
	}

	return errors.New("all strategies have failed")
}

func (e *engine) validate(request *Request) error {
	for _, requestStrategy := range request.Strategies {
		if _, exists := e.strategyMap[requestStrategy]; exists {
			if !exists {
				log.WithField("strategy", requestStrategy).Error("Requested strategy not found")
				return errors.New("requested strategy not found")
			}
		}
	}

	return nil
}

func (e *engine) buildTask(request *Request) (*Task, error) {
	id := util.RandomHexadecimalString(5)

	var sourceClient, err = k8s.GetK8sClient(request.SourceKubeconfigPath, request.SourceContext)
	if err != nil {
		return nil, err
	}

	var destClient *kubernetes.Clientset
	if request.SourceKubeconfigPath == request.DestKubeconfigPath && request.SourceContext == request.DestContext {
		destClient = sourceClient
	} else {
		destClient, err = k8s.GetK8sClient(request.DestKubeconfigPath, request.DestContext)
		if err != nil {
			return nil, err
		}
	}

	sourcePvcInfo, err := k8s.BuildPvcInfo(sourceClient, request.SourceNamespace, request.SourceName)
	if err != nil {
		return nil, err
	}

	destPvcInfo, err := k8s.BuildPvcInfo(destClient, request.DestNamespace, request.DestName)
	if err != nil {
		return nil, err
	}

	return &Task{
		Id:      id,
		Source:  sourcePvcInfo,
		Dest:    destPvcInfo,
		Options: request.Options,
	}, nil
}

func (e *engine) determineStrategies(request *Request, task *Task) []*Strategy {
	if len(request.Strategies) > 0 {
		return e.findStrategies(request.Strategies)
	}

	var strategies []*Strategy
	for _, strategy := range e.strategyMap {
		if (*strategy).CanDo(task) {
			strategies = append(strategies, strategy)
		}
	}

	sort.Slice(strategies, func(i, j int) bool {
		return (*strategies[i]).Priority() < (*strategies[j]).Priority()
	})

	return strategies
}

func (e *engine) findStrategies(strategyNames []string) []*Strategy {
	var strategies []*Strategy
	for _, strategyName := range strategyNames {
		strategy := e.strategyMap[strategyName]
		strategies = append(strategies, strategy)
	}

	return strategies
}
