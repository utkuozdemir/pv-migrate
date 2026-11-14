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

	helmVals, sshTargetHost, err := buildClusterIPHelmVals(mig, topo, releaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to build helm values: %w", err)
	}

	// This one release spans two namespaces when source and destination differ:
	// the rsync job lands in the destination's, sshd in the source's. Recording
	// the source side before the install lets mid-install peeks and install-timeout
	// diagnostics see the sshd pod too.
	if mig.SourceInfo.Claim.Namespace != mig.DestInfo.Claim.Namespace {
		attempt.DiagnosticTargets = append(attempt.DiagnosticTargets,
			migration.DiagnosticTarget{Release: releaseName, Info: mig.SourceInfo})
	}

	if err = installHelmChart(ctx, attempt, mig.DestInfo, releaseName, helmVals, logger); err != nil {
		return err
	}

	narrateConnection(logger, topo.push, sshTargetHost, 0)

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
) (map[string]any, string, error) {
	publicKey, privateKey, privateKeyMountPath, err := generateSSHKeys(mig.Request.KeyAlgorithm, logger)
	if err != nil {
		return nil, "", err
	}

	sshTargetHost := helmReleaseName + "-sshd." + topo.sshd.info.Claim.Namespace
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = formatSSHTargetHost(mig.Request.DestHostOverride)
	}

	rsyncCmdStr, err := buildRsyncCmdString(mig.Request, topo.push, sshTargetHost, 0)
	if err != nil {
		return nil, "", err
	}

	return map[string]any{
		rsyncComponent: buildRsyncHelmValues(topo.rsync, rsyncCmdStr, privateKey, privateKeyMountPath),
		sshdComponent:  buildSshdHelmValues(topo.sshd, publicKey),
	}, sshTargetHost, nil
}
