package migrator

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"github.com/utkuozdemir/pv-migrate/migration"
	"helm.sh/helm/v3/pkg/chart/loader"
)

//go:embed helm-chart.tgz
var chartBytes []byte

const (
	attemptIDLength = 5
)

type (
	strategyMapGetter   func(names []string) (map[string]strategy.Strategy, error)
	clusterClientGetter func(kubeconfigPath string, context string) (*k8s.ClusterClient, error)
)

type migrator struct {
	getKubeClient  clusterClientGetter
	getStrategyMap strategyMapGetter
}

// New creates a new migrator.
func New() *migrator {
	return &migrator{
		getKubeClient:  k8s.GetClusterClient,
		getStrategyMap: strategy.GetStrategiesMapForNames,
	}
}

func (m *migrator) Run(mig *migration.Request) error {
	nameToStrategyMap, err := m.getStrategyMap(mig.Strategies)
	if err != nil {
		return err
	}

	t, err := m.buildMigration(mig)
	if err != nil {
		return err
	}

	strs := strings.Join(mig.Strategies, ", ")
	t.Logger.
		WithField("strategies", strs).
		Infof(":thought_balloon: Will attempt %v strategies: %s",
			len(nameToStrategyMap), strs)

	for _, name := range mig.Strategies {
		id := util.RandomHexadecimalString(attemptIDLength)
		e := migration.Attempt{
			ID:                    id,
			HelmReleaseNamePrefix: "pv-migrate-" + id,
			Migration:             t,
			Logger:                t.Logger.WithField("id", id),
		}

		sLogger := e.Logger.WithField("strategy", name)
		sLogger.Infof(":helicopter: Attempting strategy: %s", name)
		s := nameToStrategyMap[name]
		accepted, runErr := s.Run(&e)
		if !accepted {
			sLogger.Infof(":fox: Strategy '%s' cannot handle this migration, "+
				"will try the next one", name)

			continue
		}

		if runErr == nil {
			sLogger.Info(":check_mark_button: Migration succeeded")

			return nil
		}

		sLogger.WithError(runErr).
			Warn(":large_orange_diamond: Migration failed with this strategy, " +
				"will try with the remaining strategies")
	}

	return errors.New("all strategies have failed")
}

func (m *migrator) buildMigration(r *migration.Request) (*migration.Migration, error) {
	chart, err := loader.LoadArchive(bytes.NewReader(chartBytes))
	if err != nil {
		return nil, err
	}

	source := r.Source
	dest := r.Dest

	sourceClient, destClient, err := m.getClusterClients(r)
	if err != nil {
		return nil, err
	}

	sourceNs := source.Namespace
	if sourceNs == "" {
		sourceNs = sourceClient.NsInContext
	}

	destNs := dest.Namespace
	if destNs == "" {
		destNs = destClient.NsInContext
	}

	sourcePvcInfo, err := pvc.New(sourceClient, sourceNs, source.Name)
	if err != nil {
		return nil, err
	}

	destPvcInfo, err := pvc.New(destClient, destNs, dest.Name)
	if err != nil {
		return nil, err
	}

	logger := r.Logger.WithFields(log.Fields{
		"source_ns": source.Namespace,
		"source":    source.Name,
		"dest_ns":   dest.Namespace,
		"dest":      dest.Name,
	})

	err = handleMountedPVCs(logger, r, sourcePvcInfo, destPvcInfo)
	if err != nil {
		return nil, err
	}

	if !(destPvcInfo.SupportsRWO || destPvcInfo.SupportsRWX) {
		return nil, errors.New("destination pvc is not writeable")
	}

	mig := migration.Migration{
		Chart:      chart,
		Request:    r,
		Logger:     logger,
		SourceInfo: sourcePvcInfo,
		DestInfo:   destPvcInfo,
	}

	return &mig, nil
}

func (m *migrator) getClusterClients(r *migration.Request) (*k8s.ClusterClient, *k8s.ClusterClient, error) {
	source := r.Source
	dest := r.Dest

	sourceClient, err := m.getKubeClient(source.KubeconfigPath, source.Context)
	if err != nil {
		return nil, nil, err
	}

	destClient := sourceClient
	if source.KubeconfigPath != dest.KubeconfigPath || source.Context != dest.Context {
		destClient, err = m.getKubeClient(dest.KubeconfigPath, dest.Context)
		if err != nil {
			return nil, nil, err
		}
	}

	return sourceClient, destClient, nil
}

func handleMountedPVCs(logger *log.Entry, r *migration.Request, sourcePvcInfo, destPvcInfo *pvc.Info) error {
	ignoreMounted := r.IgnoreMounted
	err := handleMounted(logger, sourcePvcInfo, ignoreMounted)
	if err != nil {
		return err
	}
	err = handleMounted(logger, destPvcInfo, ignoreMounted)
	if err != nil {
		return err
	}

	if !(destPvcInfo.SupportsRWO || destPvcInfo.SupportsRWX) {
		return errors.New("destination pvc is not writeable")
	}

	return nil
}

func handleMounted(logger *log.Entry, info *pvc.Info, ignoreMounted bool) error {
	if info.MountedNode == "" {
		return nil
	}

	if ignoreMounted {
		logger.Infof(":bulb: PVC %s is mounted to node %s, ignoring...",
			info.Claim.Name, info.MountedNode)

		return nil
	}

	return fmt.Errorf("PVC %s is mounted to node %s and ignore-mounted is not requested",
		info.Claim.Name, info.MountedNode)
}
