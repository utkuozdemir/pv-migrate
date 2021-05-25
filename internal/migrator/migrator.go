package migrator

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/task"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"github.com/utkuozdemir/pv-migrate/migration"
	"k8s.io/client-go/kubernetes"
	"strings"
)

type strategyMapGetter func(names []string) (map[string]strategy.Strategy, error)
type kubeClientGetter func(kubeconfigPath string, context string) (kubernetes.Interface, string, error)

type migrator struct {
	getKubeClient  kubeClientGetter
	getStrategyMap strategyMapGetter
}

// New creates a new migrator
func New() *migrator {
	return &migrator{
		getKubeClient:  k8s.GetClientAndNsInContext,
		getStrategyMap: strategy.GetStrategiesMapForNames,
	}
}

func (m *migrator) Run(mig *migration.Migration) error {
	nameToStrategyMap, err := m.getStrategyMap(mig.Strategies)
	if err != nil {
		return err
	}

	t, err := m.buildTask(mig)
	if err != nil {
		return err
	}

	logger := log.WithFields(t.LogFields)
	logger.
		WithField("strategies", strings.Join(mig.Strategies, ",")).
		Infof("Will attempt %v strategies", len(nameToStrategyMap))

	for _, name := range mig.Strategies {
		t.ID = util.RandomHexadecimalString(5)

		logger = log.WithField("strategy", name)
		logger.Info("Attempting strategy")
		s := nameToStrategyMap[name]
		accepted, runErr := s.Run(t)
		if !accepted {
			logger.Info("Strategy cannot handle this migration, will try the next one")
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

func (m *migrator) buildTask(mig *migration.Migration) (*task.Task, error) {
	source := mig.Source
	dest := mig.Dest

	var sourceClient, sourceNsInContext, err = m.getKubeClient(source.KubeconfigPath, source.Context)
	if err != nil {
		return nil, err
	}

	destClient, destNsInContext := sourceClient, sourceNsInContext
	if source.KubeconfigPath != dest.KubeconfigPath || source.Context != dest.Context {
		destClient, destNsInContext, err = m.getKubeClient(dest.KubeconfigPath, dest.Context)
		if err != nil {
			return nil, err
		}
	}

	sourceNs := source.Namespace
	if sourceNs == "" {
		sourceNs = sourceNsInContext
	}

	destNs := dest.Namespace
	if destNs == "" {
		destNs = destNsInContext
	}

	sourcePvcInfo, err := pvc.New(sourceClient, sourceNs, source.Name)
	if err != nil {
		return nil, err
	}

	destPvcInfo, err := pvc.New(destClient, destNs, dest.Name)
	if err != nil {
		return nil, err
	}

	ignoreMounted := mig.Options.IgnoreMounted
	err = handleMounted(sourcePvcInfo, ignoreMounted)
	if err != nil {
		return nil, err
	}
	err = handleMounted(destPvcInfo, ignoreMounted)
	if err != nil {
		return nil, err
	}

	if !(destPvcInfo.SupportsRWO || destPvcInfo.SupportsRWX) {
		return nil, errors.New("destination pvc is not writeable")
	}

	logFields := log.Fields{
		"source": source.Namespace + "/" + source.Name,
		"dest":   dest.Namespace + "/" + dest.Name,
	}

	t := task.Task{
		Migration:  mig,
		LogFields:  logFields,
		SourceInfo: sourcePvcInfo,
		DestInfo:   destPvcInfo,
	}

	return &t, nil
}

func handleMounted(info *pvc.Info, ignoreMounted bool) error {
	if info.MountedNode == "" {
		return nil
	}

	if ignoreMounted {
		log.Infof("PVC %s is mounted to node %s, ignoring...", info.Claim.Name, info.MountedNode)
		return nil
	}
	return fmt.Errorf("PVC %s is mounted to node %s and ignore-mounted is not requested",
		info.Claim.Name, info.MountedNode)
}
