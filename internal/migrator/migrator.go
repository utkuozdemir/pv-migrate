package migrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
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

//nolint:funlen
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

	migrationID := request.ID
	if migrationID == "" {
		migrationID = petname.Generate(2, "-")
	}

	strategies := dedup(request.Strategies)

	logger = logger.With("migration_id", migrationID)
	logger.Info("🔄 Attempting migration", "strategies", strings.Join(strategies, ","))

	for _, name := range strategies {
		str := nameToStrategyMap[name]
		releasePrefix := "pv-migrate-" + migrationID + "-" + name
		attemptLogger := logger.With("strategy", name)
		attempt := &migration.Attempt{
			ID:                    migrationID,
			HelmReleaseNamePrefix: releasePrefix,
			Migration:             mig,
		}

		attemptLogger.Info("🚁 Attempt using strategy")

		if attemptErr := runAttempt(ctx, str, attempt, attemptLogger); attemptErr != nil {
			if errors.Is(attemptErr, strategy.ErrUnaccepted) {
				attemptLogger.Info(
					"🦊 This strategy cannot handle this migration, will try the next one",
					"reason", attemptErr.Error(),
				)

				continue
			}

			attemptLogger.Warn("🔶 Migration failed with this strategy, "+
				"will try with the remaining strategies", "error", attemptErr)

			continue
		}

		if request.Detach {
			printDetachMessage(request, migrationID, name, logger)

			return nil
		}

		attemptLogger.Info("✅ Migration succeeded")

		return nil
	}

	return errors.New("all strategies failed for this migration")
}

func runAttempt(
	ctx context.Context,
	str strategy.Strategy,
	attempt *migration.Attempt,
	logger *slog.Logger,
) (runErr error) {
	defer func() {
		if attempt.Migration.Request.NoCleanup || attempt.Detached {
			logger.Info("🧹 Cleanup skipped")

			return
		}

		if attempt.Migration.Request.NoCleanupOnFailure && runErr != nil {
			logger.Info("🧹 Cleanup skipped (migration failed, resources left for inspection)")

			return
		}

		if cleanupErr := strategy.Cleanup(attempt, logger); cleanupErr != nil {
			logger.Warn("🔶 Cleanup failed, you might want to clean up manually", "error", cleanupErr)
		} else {
			logger.Info("✨ Cleanup done")
		}
	}()

	return str.Run(ctx, attempt, logger)
}

func printDetachMessage(request *migration.Request, migrationID, strategyName string, logger *slog.Logger) {
	logger.Info("🚀 Migration detached",
		"migration_id", migrationID,
		"strategy", strategyName,
	)

	fmt.Fprintln(request.Writer)
	fmt.Fprintf(request.Writer, "Migration %s detached. The rsync job is running in the cluster.\n", migrationID)
	fmt.Fprintln(request.Writer)
	fmt.Fprintln(request.Writer, "To check status:")
	fmt.Fprintf(request.Writer, "  pv-migrate status %s\n", migrationID)
	fmt.Fprintln(request.Writer)
	fmt.Fprintln(request.Writer, "To clean up after completion:")
	fmt.Fprintf(request.Writer, "  pv-migrate cleanup %s\n", migrationID)
	fmt.Fprintln(request.Writer)
}

func (m *Migrator) buildMigration(ctx context.Context, request *migration.Request,
	logger *slog.Logger,
) (*migration.Migration, error) {
	chart, err := helm.LoadChart(request.ChartVersion)
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

	if err = validatePVCs(ctx, request, sourcePvcInfo, destPvcInfo, logger); err != nil {
		return nil, err
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

// validatePVCs runs the pre-flight checks on the resolved source and
// destination PVCs before the migration is attempted.
func validatePVCs(
	ctx context.Context,
	request *migration.Request,
	sourceInfo, destInfo *pvc.Info,
	logger *slog.Logger,
) error {
	if sourceInfo == nil || sourceInfo.Claim == nil || destInfo == nil || destInfo.Claim == nil {
		return errors.New("source or destination PVC info is invalid")
	}

	if !destInfo.SupportsRWO && !destInfo.SupportsRWX {
		return errors.New("destination PVC is not writable")
	}

	return handleSizes(ctx, request, sourceInfo, destInfo, logger)
}

// handleSizes fails early when the destination PVC is smaller than the source
// PVC. Such a migration would otherwise typically fail midway with a generic
// "all strategies failed" error once the destination runs out of space.
// The check compares the resolved storage sizes (see pvc.Info.Size) and is
// skipped when --ignore-sizes is requested, when either size is unknown, or when
// either PVC's storage provisioner does not enforce the requested capacity (see
// capacityEnforced), in which case the declared sizes are meaningless.
func handleSizes(
	ctx context.Context,
	request *migration.Request,
	sourceInfo, destInfo *pvc.Info,
	logger *slog.Logger,
) error {
	sourceSize := sourceInfo.Size()
	destSize := destInfo.Size()

	if request.IgnoreSizes {
		logger.Info("💡 --ignore-sizes is requested, skipping PVC size check",
			"source_size", sourceSize.String(), "dest_size", destSize.String())

		return nil
	}

	if sourceSize.IsZero() || destSize.IsZero() {
		logger.Debug("Skipping PVC size check, capacity unknown for source or destination",
			"source_size", sourceSize.String(), "dest_size", destSize.String())

		return nil
	}

	if destSize.Cmp(sourceSize) >= 0 {
		return nil
	}

	// The destination is smaller than the source. This only leads to a failure
	// if the provisioner actually enforces the requested capacity. Many local
	// provisioners (e.g. rancher.io/local-path) ignore it, so the sizes are
	// meaningless and the check would be a false positive.
	for _, candidate := range []struct {
		role string
		info *pvc.Info
	}{
		{role: "source", info: sourceInfo},
		{role: "destination", info: destInfo},
	} {
		provisioner, err := candidate.info.Provisioner(ctx)
		if err != nil {
			logger.Debug("Could not resolve PVC storage provisioner, continuing with size check",
				"pvc", candidate.info.Claim.Namespace+"/"+candidate.info.Claim.Name, "error", err.Error())
		}

		if !capacityEnforced(provisioner) {
			logger.Info("💡 PVC storage provisioner does not enforce capacity, skipping PVC size check",
				"role", candidate.role, "provisioner", provisioner,
				"source_size", sourceSize.String(), "dest_size", destSize.String())

			return nil
		}
	}

	return fmt.Errorf("destination PVC %s/%s (%s) is smaller than source PVC %s/%s (%s): "+
		"the migration would likely fail once the destination runs out of space. "+
		"If you are sure the data fits, re-run with --ignore-sizes",
		destInfo.Claim.Namespace, destInfo.Claim.Name, destSize.String(),
		sourceInfo.Claim.Namespace, sourceInfo.Claim.Name, sourceSize.String())
}

// capacityEnforced reports whether a storage provisioner enforces the requested
// volume capacity. Several common local provisioners ignore it (rancher.io/local-path
// used by k3s/k3d/kind/OrbStack, the minikube/MicroK8s/Docker Desktop hostpath
// provisioners, OpenEBS LocalPV, etc.), so the PVC size is effectively a no-op and
// comparing source and destination sizes is meaningless. An empty or unknown
// provisioner is treated as enforcing, so the size check still runs by default.
func capacityEnforced(provisioner string) bool {
	if provisioner == "" {
		return true
	}

	p := strings.ToLower(provisioner)

	switch {
	case strings.Contains(p, "local-path"),
		strings.Contains(p, "hostpath"),
		p == "openebs.io/local":
		return false
	default:
		return true
	}
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

func dedup(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	result := make([]string, 0, len(s))

	for _, val := range s {
		if _, ok := seen[val]; ok {
			continue
		}

		seen[val] = struct{}{}
		result = append(result, val)
	}

	return result
}
