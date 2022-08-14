package migrator

import (
	"bytes"
	"context"
	_ "embed" // we embed the helm chart
	"errors"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/chart/loader"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"github.com/utkuozdemir/pv-migrate/migration"
)

var (
	//go:embed helm-chart.tgz
	chartBytes []byte

	ErrMounted             = errors.New("PVC is mounted to a node and ignore-mounted is not requested")
	ErrDestPVCNotWritable  = errors.New("destination PVC is not writable")
	ErrAllStrategiesFailed = errors.New("all strategies failed")
)

const (
	attemptIDLength = 5
)

type (
	strategyMapGetter   func(names []string) (map[string]strategy.Strategy, error)
	clusterClientGetter func(kubeconfigPath string, context string) (*k8s.ClusterClient, error)
)

type Migrator struct {
	getKubeClient  clusterClientGetter
	getStrategyMap strategyMapGetter
}

// New creates a new migrator.
func New() *Migrator {
	return &Migrator{
		getKubeClient:  k8s.GetClusterClient,
		getStrategyMap: strategy.GetStrategiesMapForNames,
	}
}

func (m *Migrator) Run(ctx context.Context, request *migration.Request) error {
	nameToStrategyMap, err := m.getStrategyMap(request.Strategies)
	if err != nil {
		return err
	}

	mig, err := m.buildMigration(ctx, request)
	if err != nil {
		return err
	}

	strs := strings.Join(request.Strategies, ", ")
	mig.Logger.
		WithField("strategies", strs).
		Infof(":thought_balloon: Will attempt %v strategies: %s",
			len(nameToStrategyMap), strs)

	for _, name := range request.Strategies {
		id := util.RandomHexadecimalString(attemptIDLength)
		attempt := migration.Attempt{
			ID:                    id,
			HelmReleaseNamePrefix: "pv-migrate-" + id,
			Migration:             mig,
			Logger:                mig.Logger.WithField("id", id),
		}

		sLogger := attempt.Logger.WithField("strategy", name)
		sLogger.Infof(":helicopter: Attempting strategy: %s", name)
		s := nameToStrategyMap[name]

		accepted, runErr := s.Run(ctx, &attempt)
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

	return ErrAllStrategiesFailed
}

func (m *Migrator) buildMigration(ctx context.Context, request *migration.Request) (*migration.Migration, error) {
	chart, err := loader.LoadArchive(bytes.NewReader(chartBytes))
	if err != nil {
		return nil, err
	}

	source := request.Source
	dest := request.Dest

	sourceClient, destClient, err := m.getClusterClients(request)
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

	sourcePvcInfo, err := pvc.New(ctx, sourceClient, sourceNs, source.Name)
	if err != nil {
		return nil, err
	}

	destPvcInfo, err := pvc.New(ctx, destClient, destNs, dest.Name)
	if err != nil {
		return nil, err
	}

	logger := request.Logger.WithFields(log.Fields{
		"source_ns": source.Namespace,
		"source":    source.Name,
		"dest_ns":   dest.Namespace,
		"dest":      dest.Name,
	})

	err = handleMountedPVCs(logger, request, sourcePvcInfo, destPvcInfo)
	if err != nil {
		return nil, err
	}

	if !(destPvcInfo.SupportsRWO || destPvcInfo.SupportsRWX) {
		return nil, ErrDestPVCNotWritable
	}

	mig := migration.Migration{
		Chart:      chart,
		Request:    request,
		Logger:     logger,
		SourceInfo: sourcePvcInfo,
		DestInfo:   destPvcInfo,
	}

	return &mig, nil
}

func (m *Migrator) getClusterClients(r *migration.Request) (*k8s.ClusterClient, *k8s.ClusterClient, error) {
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
		return ErrDestPVCNotWritable
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

	return fmt.Errorf("%w: node: %s claim %s", ErrMounted, info.MountedNode, info.Claim.Name)
}
