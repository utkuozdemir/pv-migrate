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
		return fmt.Errorf("%s: %w", reason, ErrUnaccepted)
	}

	topo := resolveTopology(mig)
	releaseName := attempt.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	helmVals, err := buildClusterIPHelmVals(mig, topo, releaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to build helm values: %w", err)
	}

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	if err = installHelmChart(attempt, mig.DestInfo, releaseName, helmVals, logger); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
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
		sshTargetHost = mig.Request.DestHostOverride
	}

	rsyncCmd := buildRsyncCmd(mig.Request, topo.push, sshTargetHost, 0)

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build rsync command: %w", err)
	}

	return map[string]any{
		"rsync": buildRsyncHelmValues(topo.rsync, rsyncCmdStr, privateKey, privateKeyMountPath),
		"sshd":  buildSshdHelmValues(topo.sshd, publicKey),
	}, nil
}
