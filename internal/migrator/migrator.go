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

type resolvedTransfer struct {
	request   *migration.Request
	migration *migration.Migration
}

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

// RunBatch executes a batch of migrations. When the loadbalancer strategy is available,
// transfers sharing a source namespace are optimised to use a single shared sshd +
// LoadBalancer service instead of one per transfer.
func (m *Migrator) RunBatch(ctx context.Context, requests []*migration.Request, logger *slog.Logger) error {
	if len(requests) == 0 {
		return nil
	}

	if len(requests) == 1 {
		return m.Run(ctx, requests[0], logger)
	}

	nameToStrategyMap, err := m.getStrategyMap(requests[0].Strategies)
	if err != nil {
		return err
	}

	// Check if loadbalancer strategy is available for batch optimisation.
	lbStratRaw, hasLB := nameToStrategyMap["loadbalancer"]
	if !hasLB {
		logger.Warn("⚠️ Batch optimisation requires loadbalancer strategy; running individual migrations")

		return m.runIndividual(ctx, requests, logger)
	}

	lbStrat, ok := lbStratRaw.(*strategy.LoadBalancer)
	if !ok {
		return errors.New("internal error: unexpected loadbalancer strategy type")
	}

	logger.Info("📦 Batch mode: grouping transfers by source namespace", "total_transfers", len(requests))

	// Build all migrations.
	transfers := make([]resolvedTransfer, 0, len(requests))

	for _, req := range requests {
		reqLogger := logger.With(
			"source", req.Source.Namespace+"/"+req.Source.Name,
			"dest", req.Dest.Namespace+"/"+req.Dest.Name,
		)

		mig, err := m.buildMigration(ctx, req, reqLogger)
		if err != nil {
			return fmt.Errorf("failed to build migration for %s: %w", req.Source.Name, err)
		}

		transfers = append(transfers, resolvedTransfer{request: req, migration: mig})
	}

	// Group by source namespace preserving order.
	type nsGroup struct {
		namespace string
		transfers []resolvedTransfer
	}

	groupMap := make(map[string]*nsGroup)
	groupOrder := make([]string, 0)

	for _, t := range transfers {
		ns := t.migration.SourceInfo.Claim.Namespace
		if _, exists := groupMap[ns]; !exists {
			groupMap[ns] = &nsGroup{namespace: ns}
			groupOrder = append(groupOrder, ns)
		}

		groupMap[ns].transfers = append(groupMap[ns].transfers, t)
	}

	for _, ns := range groupOrder {
		group := groupMap[ns]

		logger.Info("🔄 Running batch migration for source namespace",
			"namespace", ns, "transfers", len(group.transfers))

		if err := m.runBatchLB(ctx, lbStrat, group.transfers, logger); err != nil {
			return fmt.Errorf("batch migration failed for namespace %s: %w", ns, err)
		}
	}

	return nil
}

// runIndividual falls back to running each request as an independent migration.
func (m *Migrator) runIndividual(ctx context.Context, requests []*migration.Request, logger *slog.Logger) error {
	for i, req := range requests {
		logger.Info("🚁 Running migration", "transfer", fmt.Sprintf("%d/%d", i+1, len(requests)))

		if err := m.Run(ctx, req, logger); err != nil {
			return err
		}
	}

	return nil
}

// runBatchLB runs a group of transfers that share a source namespace using a
// single shared LoadBalancer source endpoint.
func (m *Migrator) runBatchLB(
	ctx context.Context,
	lbStrat *strategy.LoadBalancer,
	transfers []resolvedTransfer,
	logger *slog.Logger,
) error {
	sessionID := util.RandomString(attemptIDLength)

	// Collect all unique source PVC infos.
	allSourceInfos := make([]*pvc.Info, 0, len(transfers))

	for _, t := range transfers {
		allSourceInfos = append(allSourceInfos, t.migration.SourceInfo)
	}

	// Create a synthetic attempt for the shared source setup using the first migration.
	firstMig := transfers[0].migration

	sharedAttempt := &migration.Attempt{
		ID:                    sessionID,
		HelmReleaseNamePrefix: "pv-migrate-" + sessionID,
		Migration:             firstMig,
	}

	// Setup the shared source endpoint (one sshd + one LB service for all PVCs).
	shared, err := lbStrat.SetupSharedSource(
		ctx, sharedAttempt, allSourceInfos,
		firstMig.Request.SourceMountReadWrite, logger,
	)
	if err != nil {
		return fmt.Errorf("failed to setup shared source endpoint: %w", err)
	}

	// Ensure the shared source is cleaned up when we're done.
	noCleanup := firstMig.Request.NoCleanup

	defer func() {
		if !noCleanup {
			lbStrat.CleanupSharedSource(
				allSourceInfos[0], shared.ReleaseName,
				firstMig.Request.HelmTimeout, logger,
			)
		} else {
			logger.Info("🧹 Shared source cleanup skipped (--no-cleanup)")
		}
	}()

	// Run individual transfers against the shared source endpoint.
	for i, t := range transfers {
		transferID := fmt.Sprintf("%s-%d", sessionID, i)

		transferLogger := logger.With(
			"transfer", fmt.Sprintf("%d/%d", i+1, len(transfers)),
			"source_pvc", t.migration.SourceInfo.Claim.Name,
			"dest_pvc", t.migration.DestInfo.Claim.Name,
		)

		transferLogger.Info("🚁 Running transfer")

		srcMountPath, ok := shared.MountPaths[t.migration.SourceInfo.Claim.Name]
		if !ok {
			return fmt.Errorf("internal error: no mount path for source PVC %s",
				t.migration.SourceInfo.Claim.Name)
		}

		attempt := &migration.Attempt{
			ID:                    transferID,
			HelmReleaseNamePrefix: "pv-migrate-" + transferID,
			Migration:             t.migration,
			SourceEndpoint: &migration.SourceEndpoint{
				Address:      shared.Address,
				ReleaseName:  shared.ReleaseName,
				SrcMountPath: srcMountPath,
				PrivateKey:   shared.PrivateKey,
				KeyAlgorithm: shared.KeyAlgorithm,
			},
		}

		if runErr := lbStrat.Run(ctx, attempt, transferLogger); runErr != nil {
			return fmt.Errorf("transfer %d failed (%s → %s): %w",
				i+1, t.migration.SourceInfo.Claim.Name,
				t.migration.DestInfo.Claim.Name, runErr)
		}

		transferLogger.Info("✅ Transfer succeeded")
	}

	return nil
}
