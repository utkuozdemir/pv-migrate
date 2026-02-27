package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
)

type ClusterIP struct{}

func (r *ClusterIP) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	if !r.canDo(mig) {
		return ErrUnaccepted
	}

	releaseName := attempt.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	helmVals, err := buildHelmVals(mig, releaseName, logger)
	if err != nil {
		return fmt.Errorf("failed to build helm values: %w", err)
	}

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	err = installHelmChart(attempt, mig.DestInfo, releaseName, helmVals)
	if err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	showProgressBar := mig.Request.ShowProgressBar
	kubeClient := mig.SourceInfo.ClusterClient.KubeClient
	jobName := releaseName + "-rsync"

	if err = k8s.WaitForJobCompletion(ctx, kubeClient,
		mig.DestInfo.Claim.Namespace, jobName, showProgressBar, mig.Request.Writer, logger); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}

func (r *ClusterIP) canDo(t *migration.Migration) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient.RestConfig.Host == d.ClusterClient.RestConfig.Host

	return sameCluster
}

//
//nolint:funlen
func buildHelmVals(
	mig *migration.Migration,
	helmReleaseName string,
	logger *slog.Logger,
) (map[string]any, error) {
	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo
	sourceNs := sourceInfo.Claim.Namespace
	destNs := destInfo.Claim.Namespace
	keyAlgorithm := mig.Request.KeyAlgorithm

	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh key pair: %w", err)
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	sshTargetHost := helmReleaseName + "-sshd." + sourceNs
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    mig.Request.NoChown,
		Delete:     mig.Request.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSSH:  true,
		SrcSSHHost: sshTargetHost,
		Compress:   !mig.Request.NoCompress,
	}

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build rsync command: %w", err)
	}

	return map[string]any{
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           destNs,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"pvcMounts": []map[string]any{
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command":  rsyncCmdStr,
			"affinity": destInfo.AffinityHelmValues,
		},
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": sourceNs,
			"publicKey": publicKey,
			"pvcMounts": []map[string]any{
				{
					"name":      sourceInfo.Claim.Name,
					"mountPath": srcMountPath,
					"readOnly":  !mig.Request.SourceMountReadWrite,
				},
			},
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}, nil
}
