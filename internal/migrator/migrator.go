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

	// The story opens with what is being migrated, once the namespaces are
	// known, and the facts about it follow as details: the identifier, the two
	// claims, what was asked for, and the order the strategies are tried in.
	logger.Info(fmt.Sprintf("🚀 Migrating %s/%s to %s/%s", sourceNs, request.Source.Name, destNs, request.Dest.Name))

	details := narrate.Detail(logger, 1)
	details.Info(fmt.Sprintf("🆔 migration id %s, for status and cleanup", migrationID))

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

		logger.Info(describeAttempt(name, request.Push))

		last := strategyIndex == len(strategies)-1

		if attemptErr := runAttempt(ctx, str, attempt, name, last, logger); attemptErr != nil {
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
		}

		return nil
	}

	reportOutcomes(request, outcomes, logger)

	return newLadderExhaustedError(outcomes)
}

// describeAttempt announces a strategy with what it does, when it has a
// description to give.
func describeAttempt(name string, push bool) string {
	if description := strategy.Describe(name, push); description != "" {
		return "🚁 " + name + ": " + description
	}

	return "🚁 " + name
}

// describeMigration tells the facts the run starts from: the two claims, how
// they relate, what was asked for, and the order of the strategies.
func describeMigration(mig *migration.Migration, strategies []string, details *slog.Logger) {
	if mig.Request.DeleteExtraneousFiles {
		details.Info("❕ files missing on the source will be deleted from the destination")
	}

	if len(strategies) == 1 {
		details.Info("🧭 trying " + strategies[0] + " only")
	} else {
		details.Info("🧭 trying " + strings.Join(strategies, ", ") + ", in that order")
	}
}

// describeRelation says how the two claims sit relative to each other, which
// is what decides the cheapest strategy that can apply.
func describeRelation(sourceInfo, destInfo *pvc.Info) string {
	source, dest := sourceInfo.ClusterClient, destInfo.ClusterClient
	sameCluster := source != nil && dest != nil && source.RestConfig != nil && dest.RestConfig != nil &&
		source.RestConfig.Host == dest.RestConfig.Host

	switch {
	case !sameCluster:
		return "🏠 the claims are in different clusters"
	case sourceInfo.Claim.Namespace == destInfo.Claim.Namespace:
		return "🏠 both claims are in the same cluster and namespace"
	default:
		return "🏠 both claims are in the same cluster, in different namespaces"
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
		details.Info("🦊 does not apply: " + declined.Reason)

		return
	}

	if errors.Is(attemptErr, strategy.ErrUnaccepted) {
		details.Info("🦊 does not apply")

		return
	}

	// On the last attempt the summary repeats the error a few lines below, so in
	// text mode the mid-run line skips it rather than showing the same sentence
	// twice on one screen. Mid-ladder, and always on a structured stream, this
	// line is the only timely record.
	if last && !structuredLogs {
		details.Warn("🔶 failed, see below")
	} else {
		details.Warn("🔶 failed: " + attemptErr.Error())
	}
}

func runAttempt(
	ctx context.Context,
	str strategy.Strategy,
	attempt *migration.Attempt,
	name string,
	last bool,
	logger *slog.Logger,
) (runErr error) {
	defer func() { cleanupAttempt(attempt, runErr, logger) }()

	started := time.Now()

	runErr = str.Run(ctx, attempt, logger)
	if runErr != nil {
		narrateFailedAttempt(runErr, last, attempt.Migration.Request.StructuredLogs, logger)
	}

	// The success is said before the cleanup that follows it, since the cleanup
	// is part of the story of a finished transfer, not of a new step.
	if runErr == nil && !attempt.Detached {
		logger.Info(fmt.Sprintf("✅ Migration succeeded over %s in %s", name, time.Since(started).Round(time.Second)))
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
		details.Info("🧹 cleanup skipped, the resources stay in the cluster")
	case attempt.Migration.Request.NoCleanupOnFailure && runErr != nil:
		details.Info("🧹 cleanup skipped since the migration failed, the resources stay for inspection")
	default:
		if cleanupErr := strategy.Cleanup(attempt, logger); cleanupErr != nil {
			details.Warn("🔶 cleanup failed, clean up with pv-migrate cleanup: " + cleanupErr.Error())
		}
	}
}

func printDetachMessage(request *migration.Request, migrationID, strategyName string, logger *slog.Logger) {
	logger.Info(fmt.Sprintf("🚀 Migration detached, the rsync job of the %s strategy keeps running in the cluster",
		strategyName))

	fmt.Fprintln(request.Writer)
	fmt.Fprintf(request.Writer, "Migration %s detached. The rsync job is running in the cluster.\n", migrationID)
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
	logger.Info("📌 source " + sourcePvcInfo.Describe())
	logger.Info("📌 destination " + destPvcInfo.Describe())
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
		logger.Info("💡 skipping the size check because --ignore-sizes is set")

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
			logger.Info(fmt.Sprintf("💡 the %s's provisioner %s does not enforce capacity, so the size check is skipped",
				candidate.role, provisioner))

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
		logger.Info(fmt.Sprintf("💡 %s/%s is mounted on node %s, continuing because --ignore-mounted is set",
			info.Claim.Namespace, info.Claim.Name, info.MountedNode))

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
