package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

type Mount struct{}

//nolint:funlen
func (r *Mount) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	if !r.canDo(mig) {
		return ErrUnaccepted
	}

	sourceInfo := attempt.Migration.SourceInfo
	destInfo := attempt.Migration.DestInfo
	namespace := sourceInfo.Claim.Namespace

	node := determineTargetNode(mig)

	rsyncCmd, err := buildRsyncCmdMount(mig)
	if err != nil {
		return fmt.Errorf("failed to build rsync command: %w", err)
	}

	vals := map[string]any{
		"rsync": map[string]any{
			"enabled":   true,
			"namespace": namespace,
			"nodeName":  node,
			"pvcMounts": []map[string]any{
				{
					"name":      sourceInfo.Claim.Name,
					"mountPath": srcMountPath,
					"readOnly":  !mig.Request.SourceMountReadWrite,
				},
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command":  rsyncCmd,
			"affinity": sourceInfo.AffinityHelmValues,
		},
	}

	releaseName := attempt.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	doneCh := registerCleanupHook(attempt, releaseNames, logger)
	defer cleanupAndReleaseHook(ctx, attempt, releaseNames, doneCh, logger)

	err = installHelmChart(attempt, sourceInfo, releaseName, vals)
	if err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	showProgressBar := mig.Request.ShowProgressBar
	kubeClient := mig.SourceInfo.ClusterClient.KubeClient
	jobName := attempt.HelmReleaseNamePrefix + "-rsync"

	if err = k8s.WaitForJobCompletion(
		ctx,
		kubeClient,
		namespace,
		jobName,
		showProgressBar,
		mig.Request.Writer,
		logger,
	); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}

func (r *Mount) canDo(t *migration.Migration) bool {
	sourceInfo := t.SourceInfo
	destInfo := t.DestInfo

	sameCluster := sourceInfo.ClusterClient.RestConfig.Host == destInfo.ClusterClient.RestConfig.Host
	if !sameCluster {
		return false
	}

	sameNamespace := sourceInfo.Claim.Namespace == destInfo.Claim.Namespace
	if !sameNamespace {
		return false
	}

	sameNode := sourceInfo.MountedNode == destInfo.MountedNode
	oneUnmounted := sourceInfo.MountedNode == "" || destInfo.MountedNode == ""

	return sameNode || oneUnmounted || sourceInfo.SupportsROX || sourceInfo.SupportsRWX ||
		destInfo.SupportsRWX
}

func buildRsyncCmdMount(mig *migration.Migration) (string, error) {
	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:  mig.Request.NoChown,
		Delete:   mig.Request.DeleteExtraneousFiles,
		SrcPath:  srcPath,
		DestPath: destPath,
		Compress: !mig.Request.NoCompress,
	}

	cmd, err := rsyncCmd.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build rsync command: %w", err)
	}

	return cmd, nil
}

func determineTargetNode(t *migration.Migration) string {
	sourceInfo := t.SourceInfo
	destInfo := t.DestInfo

	if sourceInfo.MountedNode != "" && !sourceInfo.SupportsROX && !sourceInfo.SupportsRWX {
		return sourceInfo.MountedNode
	}

	if destInfo.MountedNode != "" && !destInfo.SupportsROX && !destInfo.SupportsRWX {
		return destInfo.MountedNode
	}

	if sourceInfo.MountedNode != "" {
		return sourceInfo.MountedNode
	}

	return destInfo.MountedNode
}
