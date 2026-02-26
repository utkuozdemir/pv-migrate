package migrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

const (
	attemptIDLength = 5
)

type (
	strategyMapGetter   func(names []string) (map[string]strategy.Strategy, error)
	clusterClientGetter func(kubeconfigPath, context string, logger *slog.Logger) (*k8s.ClusterClient, error)
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

func (m *Migrator) Run(ctx context.Context, request *migration.Request, logger *slog.Logger) error {
	nameToStrategyMap, err := m.getStrategyMap(request.Strategies)
	if err != nil {
		return err
	}

	logger = logger.With("source", request.Source.Namespace+"/"+request.Source.Name,
		"dest", request.Dest.Namespace+"/"+request.Dest.Name)

	mig, err := m.buildMigration(ctx, request, logger)
	if err != nil {
		return err
	}

	logger.Info("💭 Attempting migration", "strategies", strings.Join(request.Strategies, ","))

	for _, name := range request.Strategies {
		attemptID := util.RandomString(attemptIDLength)

		attemptLogger := logger.With("attempt_id", attemptID, "strategy", name)

		attemptLogger.Info("🚁 Attempt using strategy")

		attempt := migration.Attempt{
			ID:                    attemptID,
			HelmReleaseNamePrefix: "pv-migrate-" + attemptID,
			Migration:             mig,
		}

		s := nameToStrategyMap[name]

		if runErr := s.Run(ctx, &attempt, attemptLogger); runErr != nil {
			if errors.Is(runErr, strategy.ErrUnaccepted) {
				attemptLogger.Info(
					"🦊 This strategy cannot handle this migration, will try the next one",
				)

				continue
			}

			attemptLogger.Warn("🔶 Migration failed with this strategy, "+
				"will try with the remaining strategies", "error", runErr)

			continue
		}

		attemptLogger.Info("✅ Migration succeeded")

		return nil
	}

	return errors.New("all strategies failed for this migration")
}

func (m *Migrator) buildMigration(ctx context.Context, request *migration.Request,
	logger *slog.Logger,
) (*migration.Migration, error) {
	chart, err := helm.LoadChart()
	if err != nil {
		return nil, fmt.Errorf("failed to load helm chart: %w", err)
	}

	source := request.Source
	dest := request.Dest

	sourceClient, destClient, err := m.getClusterClients(request, logger)
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
		return nil, fmt.Errorf("failed to get PVC info for source PVC: %w", err)
	}

	destPvcInfo, err := pvc.New(ctx, destClient, destNs, dest.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC info for destination PVC: %w", err)
	}

	err = handleMountedPVCs(request, sourcePvcInfo, destPvcInfo, logger)
	if err != nil {
		return nil, err
	}

	if !destPvcInfo.SupportsRWO && !destPvcInfo.SupportsRWX {
		return nil, errors.New("destination PVC is not writable")
	}

	mig := migration.Migration{
		Chart:      chart,
		Request:    request,
		SourceInfo: sourcePvcInfo,
		DestInfo:   destPvcInfo,
	}

	return &mig, nil
}

func (m *Migrator) getClusterClients(r *migration.Request,
	logger *slog.Logger,
) (*k8s.ClusterClient, *k8s.ClusterClient, error) {
	source := r.Source
	dest := r.Dest

	sourceClient, err := m.getKubeClient(source.KubeconfigPath, source.Context, logger)
	if err != nil {
		return nil, nil, err
	}

	destClient := sourceClient
	if source.KubeconfigPath != dest.KubeconfigPath || source.Context != dest.Context {
		destClient, err = m.getKubeClient(dest.KubeconfigPath, dest.Context, logger)
		if err != nil {
			return nil, nil, err
		}
	}

	return sourceClient, destClient, nil
}

func handleMountedPVCs(
	r *migration.Request,
	sourcePvcInfo, destPvcInfo *pvc.Info,
	logger *slog.Logger,
) error {
	ignoreMounted := r.IgnoreMounted

	err := handleMounted(sourcePvcInfo, ignoreMounted, logger)
	if err != nil {
		return err
	}

	err = handleMounted(destPvcInfo, ignoreMounted, logger)
	if err != nil {
		return err
	}

	return nil
}

func handleMounted(info *pvc.Info, ignoreMounted bool, logger *slog.Logger) error {
	if info.MountedNode == "" {
		return nil
	}

	if ignoreMounted {
		logger.Info("💡 PVC is mounted to a node, but --ignore-mounted is requested, ignoring...",
			"pvc", info.Claim.Namespace+"/"+info.Claim.Name, "mounted_node", info.MountedNode)

		return nil
	}

	return fmt.Errorf("PVC is mounted to a node and --ignore-mounted is not requested: "+
		"node: %s claim %s", info.MountedNode, info.Claim.Name)
}
