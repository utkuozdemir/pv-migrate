package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

type ClusterIP struct{}

func (r *ClusterIP) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	if reason := r.cannotDoReason(mig); reason != "" {
		return Declined(reason)
	}

	topo := resolveTopology(mig)
	releaseName := attempt.HelmReleaseNamePrefix
	attempt.ReleaseNames = []string{releaseName}

	helmVals, err := buildClusterIPHelmVals(mig, topo, releaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to build helm values: %w", err)
	}

	if err = installHelmChart(ctx, attempt, mig.DestInfo, releaseName, helmVals, logger); err != nil {
		return err
	}

	// This one release spans two namespaces when source and destination differ:
	// the rsync job lands in the destination's, sshd in the source's. The install
	// records the former, so the sshd side has to be added for diagnostics to
	// see it.
	if mig.SourceInfo.Claim.Namespace != mig.DestInfo.Claim.Namespace {
		attempt.DiagnosticTargets = append(attempt.DiagnosticTargets,
			migration.DiagnosticTarget{Release: releaseName, Info: mig.SourceInfo})
	}

	return waitForRsyncJob(ctx, attempt, topo.rsync.info, releaseName, logger)
}

func (r *ClusterIP) cannotDoReason(t *migration.Migration) string {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient.RestConfig.Host == d.ClusterClient.RestConfig.Host

	if !sameCluster {
		return "source and destination are on different clusters"
	}

	return ""
}

func buildClusterIPHelmVals(
	mig *migration.Migration,
	topo topology,
	helmReleaseName string,
	logger *slog.Logger,
) (map[string]any, error) {
	publicKey, privateKey, privateKeyMountPath, err := generateSSHKeys(mig.Request.KeyAlgorithm, logger)
	if err != nil {
		return nil, err
	}

	sshTargetHost := helmReleaseName + "-sshd." + topo.sshd.info.Claim.Namespace
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = formatSSHTargetHost(mig.Request.DestHostOverride)
	}

	rsyncCmdStr, err := buildRsyncCmdString(mig.Request, topo.push, sshTargetHost, 0)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		rsyncComponent: buildRsyncHelmValues(topo.rsync, rsyncCmdStr, privateKey, privateKeyMountPath),
		sshdComponent:  buildSshdHelmValues(topo.sshd, publicKey),
	}, nil
}
