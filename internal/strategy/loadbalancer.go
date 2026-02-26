package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

type LoadBalancer struct{}

//nolint:funlen
func (r *LoadBalancer) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration

	sourceInfo := mig.SourceInfo
	destInfo := mig.DestInfo
	sourceNs := sourceInfo.Claim.Namespace
	destNs := destInfo.Claim.Namespace
	keyAlgorithm := mig.Request.KeyAlgorithm

	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return fmt.Errorf("failed to create ssh key pair: %w", err)
	}

	privateKeyMountPath := "/tmp/id_" + keyAlgorithm

	srcReleaseName := attempt.HelmReleaseNamePrefix + "-src"
	destReleaseName := attempt.HelmReleaseNamePrefix + "-dest"
	releaseNames := []string{srcReleaseName, destReleaseName}

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	err = installOnSource(attempt, srcReleaseName, publicKey, srcMountPath)
	if err != nil {
		return fmt.Errorf("failed to install on source: %w", err)
	}

	sourceKubeClient := attempt.Migration.SourceInfo.ClusterClient.KubeClient
	svcName := srcReleaseName + "-sshd"

	lbAddress, err := k8s.GetServiceAddress(
		ctx,
		sourceKubeClient,
		sourceNs,
		svcName,
		mig.Request.LoadBalancerTimeout,
	)
	if err != nil {
		return fmt.Errorf("failed to get service address: %w", err)
	}

	sshTargetHost := formatSSHTargetHost(lbAddress)
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	err = installOnDest(attempt, destReleaseName, privateKey, privateKeyMountPath,
		sshTargetHost, srcMountPath, destMountPath)
	if err != nil {
		return fmt.Errorf("failed to install on dest: %w", err)
	}

	showProgressBar := attempt.Migration.Request.ShowProgressBar
	kubeClient := destInfo.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"

	if err = k8s.WaitForJobCompletion(
		ctx,
		kubeClient,
		destNs,
		jobName,
		showProgressBar,
		mig.Request.Writer,
		logger,
	); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}

func installOnSource(attempt *migration.Attempt, releaseName, publicKey, srcMountPath string) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	namespace := sourceInfo.Claim.Namespace

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": namespace,
			"publicKey": publicKey,
			"service": map[string]any{
				"type": "LoadBalancer",
			},
			"pvcMounts": []map[string]any{
				{
					"name":      sourceInfo.Claim.Name,
					"readOnly":  mig.Request.SourceMountReadOnly,
					"mountPath": srcMountPath,
				},
			},
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}

	return installHelmChart(attempt, sourceInfo, releaseName, vals)
}

func installOnDest(
	attempt *migration.Attempt,
	releaseName, privateKey, privateKeyMountPath, sshHost, srcMountPath, destMountPath string,
) error {
	mig := attempt.Migration
	destInfo := mig.DestInfo
	namespace := destInfo.Claim.Namespace

	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path
	rsyncCmd := rsync.Cmd{
		NoChown:    mig.Request.NoChown,
		Delete:     mig.Request.DeleteExtraneousFiles,
		SrcPath:    srcPath,
		DestPath:   destPath,
		SrcUseSSH:  true,
		SrcSSHHost: sshHost,
		Compress:   mig.Request.Compress,
	}

	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	vals := map[string]any{
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           namespace,
			"privateKeyMount":     true,
			"privateKey":          privateKey,
			"privateKeyMountPath": privateKeyMountPath,
			"sshRemoteHost":       sshHost,
			"pvcMounts": []map[string]any{
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command":  rsyncCmdStr,
			"affinity": destInfo.AffinityHelmValues,
		},
	}

	return installHelmChart(attempt, destInfo, releaseName, vals)
}

func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}

	return host
}
