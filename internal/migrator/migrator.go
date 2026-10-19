package migrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
	"github.com/utkuozdemir/pv-migrate/internal/opid"
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

	// Only the public API defaults the writer, so a direct caller can leave it
	// unset. Everything below writes to it without checking.
	if request.Writer == nil {
		request.Writer = io.Discard
	}

	migrationID := request.ID
	if migrationID == "" {
		migrationID = opid.Generate()
	}

	strategies := dedup(request.Strategies)

	sourceClient, destClient, err := m.getClusterClients(request, logger)
	if err != nil {
		return err
	}

	sourceNs, destNs := resolvedNamespaces(request, sourceClient, destClient)

	logger.Info("🚀 Migrating", "source", sourceNs+"/"+request.Source.Name, "dest", destNs+"/"+request.Dest.Name,
		"id", migrationID)

	details := narrate.Detail(logger, 1)

	mig, err := m.buildMigrationWithClients(ctx, request, sourceClient, destClient, details)
	if err != nil {
		return err
	}

	describeMigration(mig, strategies, details)

	outcomes := make([]attemptOutcome, 0, len(strategies))

	for strategyIndex, name := range strategies {
		str := nameToStrategyMap[name]
		releasePrefix := opid.ReleasePrefix + migrationID + "-" + name
		attempt := &migration.Attempt{
			ID:                    migrationID,
			HelmReleaseNamePrefix: releasePrefix,
			Migration:             mig,
		}

		logger.Info("🚁 Trying strategy", append([]any{"strategy", name}, strategy.Describe(name, request.Push)...)...)

		last := strategyIndex == len(strategies)-1
		started := time.Now()

		if attemptErr := runAttempt(ctx, str, attempt, last, logger); attemptErr != nil {
			outcomes = append(outcomes, recordFailedAttempt(name, attempt, attemptErr))

			// An interrupted run must not walk the remaining rungs: each failed
			// attempt would sweep diagnostics on a context that survives the
			// cancellation, turning one Ctrl-C into a long goodbye.
			if ctx.Err() != nil {
				break
			}

			continue
		}

		if request.Detach {
			printDetachMessage(request, migrationID, name, logger)

			return nil
		}

		// Said after the cleanup, so the run ends on its verdict.
		logger.Info("✅ Migration succeeded", "strategy", name, "duration", time.Since(started).Round(time.Second))

		return nil
	}

	reportOutcomes(request, outcomes, logger)

	return newLadderExhaustedError(outcomes)
}

// describeMigration tells the facts the run starts from: the two claims, how
// they relate, what was asked for, and the order of the strategies.
func describeMigration(mig *migration.Migration, strategies []string, details *slog.Logger) {
	if mig.Request.DeleteExtraneousFiles {
		details.Info("❕ Files missing on the source will be deleted from the destination")
	}

	details.Info("🧭 Strategies to try", "strategies", strings.Join(strategies, ","))
}

// describeRelation says how the two claims sit relative to each other, which
// is what decides the cheapest strategy that can apply.
func describeRelation(sourceInfo, destInfo *pvc.Info) string {
	source, dest := sourceInfo.ClusterClient, destInfo.ClusterClient
	sameCluster := source != nil && dest != nil && source.RestConfig != nil && dest.RestConfig != nil &&
		source.RestConfig.Host == dest.RestConfig.Host

	switch {
	case !sameCluster:
		return "🏠 Claims are in different clusters"
	case sourceInfo.Claim.Namespace == destInfo.Claim.Namespace:
		return "🏠 Claims are in the same namespace"
	default:
		return "🏠 Claims are in different namespaces of the same cluster"
	}
}

// recordFailedAttempt keeps what is needed to explain the attempt again once
// the ladder is exhausted. The attempt narrated its own outcome already.
func recordFailedAttempt(name string, attempt *migration.Attempt, attemptErr error) attemptOutcome {
	if errors.Is(attemptErr, strategy.ErrUnaccepted) {
		return attemptOutcome{strategy: name, declined: true, err: attemptErr}
	}

	return attemptOutcome{strategy: name, err: attemptErr, diagnostics: attempt.Diagnostics}
}

// narrateFailedAttempt says how the attempt ended, under the attempt's own
// step and before its cleanup, so the line sits where it belongs.
func narrateFailedAttempt(attemptErr error, last, structuredLogs bool, logger *slog.Logger) {
	details := narrate.Detail(logger, 1)

	if declined, ok := errors.AsType[*strategy.DeclinedError](attemptErr); ok {
		// The reason is the typed one, without the error sentinel's suffix. The
		// next attempt's own line says that one follows.
		details.Info("🦊 Strategy does not apply", "reason", declined.Reason)

		return
	}

	if errors.Is(attemptErr, strategy.ErrUnaccepted) {
		details.Info("🦊 Strategy does not apply")

		return
	}

	// On the last attempt in text mode, the end of the run reports the error once.
	if last && !structuredLogs {
		details.Warn("🔶 Strategy failed")
	} else {
		details.Warn("🔶 Strategy failed", "error", attemptErr)
	}
}

func runAttempt(
	ctx context.Context,
	str strategy.Strategy,
	attempt *migration.Attempt,
	last bool,
	logger *slog.Logger,
) (runErr error) {
	defer func() { cleanupAttempt(attempt, runErr, logger) }()

	runErr = str.Run(ctx, attempt, logger)
	if runErr != nil {
		narrateFailedAttempt(runErr, last, attempt.Migration.Request.StructuredLogs, logger)
	}

	// A decline never reached the cluster, so there is nothing to ask it about.
	// Anything else is collected here, the one point that sees every strategy's
	// failure while the attempt's resources still exist.
	if runErr != nil && !errors.Is(runErr, strategy.ErrUnaccepted) {
		attempt.Diagnostics = collectDiagnostics(ctx, attempt, logger)
	}

	return runErr
}

// cleanupAttempt removes what the attempt installed, unless told not to, and
// narrates as details of whatever step stands: the success line when the
// attempt worked, the attempt itself when it did not.
func cleanupAttempt(attempt *migration.Attempt, runErr error, logger *slog.Logger) {
	// A declined strategy installed nothing, so there is nothing to clean up
	// and nothing worth announcing about it.
	if len(attempt.ReleaseNames) == 0 {
		return
	}

	details := narrate.Detail(logger, 1)

	switch {
	case attempt.Migration.Request.NoCleanup || attempt.Detached:
		details.Info("🧹 Skipped cleanup, the resources stay in the cluster")
	case attempt.Migration.Request.NoCleanupOnFailure && runErr != nil:
		details.Info("🧹 Skipped cleanup because the migration failed, the resources stay for inspection")
	default:
		if cleanupErr := strategy.Cleanup(attempt, logger); cleanupErr != nil {
			details.Warn("🔶 Cleanup failed, run pv-migrate cleanup to remove the resources", "error", cleanupErr)
		}
	}
}

func printDetachMessage(request *migration.Request, migrationID, strategyName string, logger *slog.Logger) {
	logger.Info("🚀 Migration detached, the rsync job keeps running in the cluster", "strategy", strategyName)

	fmt.Fprintln(request.Writer)
	fmt.Fprintln(request.Writer, "To check status:")
	fmt.Fprintf(request.Writer, "  pv-migrate status %s\n", migrationID)
	fmt.Fprintln(request.Writer)
	fmt.Fprintln(request.Writer, "To clean up after completion:")
	fmt.Fprintf(request.Writer, "  pv-migrate cleanup %s\n", migrationID)
}

func (m *Migrator) buildMigration(ctx context.Context, request *migration.Request,
	logger *slog.Logger,
) (*migration.Migration, error) {
	sourceClient, destClient, err := m.getClusterClients(request, logger)
	if err != nil {
		return nil, err
	}

	return m.buildMigrationWithClients(ctx, request, sourceClient, destClient, logger)
}

// resolvedNamespaces fills a namespace the request left empty from the
// kubeconfig context of that side.
func resolvedNamespaces(request *migration.Request, sourceClient, destClient *k8s.ClusterClient) (string, string) {
	sourceNs := request.Source.Namespace
	if sourceNs == "" {
		sourceNs = sourceClient.NsInContext
	}

	destNs := request.Dest.Namespace
	if destNs == "" {
		destNs = destClient.NsInContext
	}

	return sourceNs, destNs
}

func (m *Migrator) buildMigrationWithClients(
	ctx context.Context,
	request *migration.Request,
	sourceClient, destClient *k8s.ClusterClient,
	logger *slog.Logger,
) (*migration.Migration, error) {
	chart, err := helm.LoadChart(request.ChartVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to load helm chart: %w", err)
	}

	source := request.Source
	dest := request.Dest
	sourceNs, destNs := resolvedNamespaces(request, sourceClient, destClient)

	sourcePvcInfo, err := pvc.New(ctx, sourceClient, sourceNs, source.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC info for source PVC: %w", err)
	}

	destPvcInfo, err := pvc.New(ctx, destClient, destNs, dest.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC info for destination PVC: %w", err)
	}

	// The facts about the claims come before the checks that qualify them, so an
	// exception reads as one.
	logger.Info("📌 Source claim", sourcePvcInfo.LogAttrs()...)
	logger.Info("📌 Destination claim", destPvcInfo.LogAttrs()...)
	logger.Info(describeRelation(sourcePvcInfo, destPvcInfo))

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
		logger.Info("💡 Skipping the size check because --ignore-sizes is set")

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
			logger.Debug("Could not resolve the PVC storage provisioner, continuing with the size check",
				"pvc", candidate.info.Claim.Namespace+"/"+candidate.info.Claim.Name, "error", err.Error())
		}

		if !capacityEnforced(provisioner) {
			logger.Info("💡 Skipping the size check, the provisioner does not enforce capacity",
				"side", candidate.role, "provisioner", provisioner)

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
		logger.Info("💡 Claim is mounted, continuing because --ignore-mounted is set",
			"pvc", info.Claim.Namespace+"/"+info.Claim.Name, "mounted_on", info.MountedNode)

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
