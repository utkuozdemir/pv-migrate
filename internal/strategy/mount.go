package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

type Mount struct{}

func (r *Mount) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	if reason := r.cannotDoReason(mig); reason != "" {
		return fmt.Errorf("%s: %w", reason, ErrUnaccepted)
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
	attempt.ReleaseNames = []string{releaseName}

	err = installHelmChart(attempt, sourceInfo, releaseName, vals, logger)
	if err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return waitForRsyncJob(ctx, attempt, sourceInfo, releaseName, logger)
}

func (r *Mount) cannotDoReason(t *migration.Migration) string {
	sourceInfo := t.SourceInfo
	destInfo := t.DestInfo

	sameCluster := sourceInfo.ClusterClient.RestConfig.Host == destInfo.ClusterClient.RestConfig.Host
	if !sameCluster {
		return "source and destination are on different clusters"
	}

	sameNamespace := sourceInfo.Claim.Namespace == destInfo.Claim.Namespace
	if !sameNamespace {
		return "source and destination are in different namespaces"
	}

	sameNode := sourceInfo.MountedNode == destInfo.MountedNode
	oneUnmounted := sourceInfo.MountedNode == "" || destInfo.MountedNode == ""

	if sameNode || oneUnmounted || sourceInfo.SupportsROX || sourceInfo.SupportsRWX ||
		destInfo.SupportsRWX {
		return ""
	}

	return "PVCs are mounted on different nodes and do not support multi-access modes"
}

func buildRsyncCmdMount(mig *migration.Migration) (string, error) {
	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:   mig.Request.NoChown,
		NonRoot:   mig.Request.NonRoot,
		Delete:    mig.Request.DeleteExtraneousFiles,
		SrcPath:   srcPath,
		DestPath:  destPath,
		Compress:  !mig.Request.NoCompress,
		ExtraArgs: mig.Request.RsyncExtraArgs,
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
