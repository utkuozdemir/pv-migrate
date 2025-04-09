// filepath: /home/joshd/git/pv-migrate/strategy/nodeport.go
package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/rsync"
	"github.com/utkuozdemir/pv-migrate/ssh"
)

type NodePort struct{}

//nolint:funlen
func (r *NodePort) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
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

	err = installNodePortOnSource(attempt, srcReleaseName, publicKey, srcMountPath, logger)
	if err != nil {
		return fmt.Errorf("failed to install on source: %w", err)
	}

	sourceKubeClient := attempt.Migration.SourceInfo.ClusterClient.KubeClient
	svcName := srcReleaseName + "-sshd"

	// Get NodePort service details
	nodeIp, nodePort, err := k8s.GetNodePortServiceDetails(
		ctx,
		sourceKubeClient,
		sourceNs,
		svcName,
		mig.Request.LBSvcTimeout,
	)
	if err != nil {
		return fmt.Errorf("failed to get NodePort service details: %w", err)
	}

	sshTargetHost := nodeIp
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = mig.Request.DestHostOverride
	}

	err = installOnDestWithNodePort(attempt, destReleaseName, privateKey, privateKeyMountPath,
		sshTargetHost, nodePort, srcMountPath, destMountPath, logger)
	if err != nil {
		return fmt.Errorf("failed to install on dest: %w", err)
	}

	showProgressBar := !attempt.Migration.Request.NoProgressBar
	kubeClient := destInfo.ClusterClient.KubeClient
	jobName := destReleaseName + "-rsync"

	if err = k8s.WaitForJobCompletion(ctx, kubeClient, destNs, jobName, showProgressBar, logger); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}

func installNodePortOnSource(attempt *migration.Attempt, releaseName,
	publicKey, srcMountPath string, logger *slog.Logger,
) error {
	mig := attempt.Migration
	sourceInfo := mig.SourceInfo
	namespace := sourceInfo.Claim.Namespace

	vals := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": namespace,
			"publicKey": publicKey,
			"service": map[string]any{
				"type": "NodePort",
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

	return installHelmChart(attempt, sourceInfo, releaseName, vals, logger)
}

func installOnDestWithNodePort(attempt *migration.Attempt, releaseName, privateKey,
	privateKeyMountPath, sshHost string, sshPort int, srcMountPath, destMountPath string, logger *slog.Logger,
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
		Port:       sshPort,
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
			"sshRemotePort":       sshPort,
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

	return installHelmChart(attempt, destInfo, releaseName, vals, logger)
}