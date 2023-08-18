package strategy

import (
	"fmt"

	"github.com/utkuozdemir/pv-migrate/k8s"
	"github.com/utkuozdemir/pv-migrate/migration"
	"github.com/utkuozdemir/pv-migrate/rsync"
)

type Mnt2 struct{}

func (r *Mnt2) canDo(t *migration.Migration) bool {
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

	return sameNode || sourceInfo.SupportsROX || sourceInfo.SupportsRWX || destInfo.SupportsRWX
}

func (r *Mnt2) Run(attempt *migration.Attempt) error {
	mig := attempt.Migration
	if !r.canDo(mig) {
		return ErrUnaccepted
	}

	sourceInfo := attempt.Migration.SourceInfo
	destInfo := attempt.Migration.DestInfo
	namespace := sourceInfo.Claim.Namespace

	node := determineTargetNode(mig)

	rsyncCmd, err := buildRsyncCmdMnt2(mig)
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
					"readOnly":  mig.Request.SourceMountReadOnly,
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

	doneCh := registerCleanupHook(attempt, releaseNames)
	defer cleanupAndReleaseHook(attempt, releaseNames, doneCh)

	err = installHelmChart(attempt, sourceInfo, releaseName, vals)
	if err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	showProgressBar := !mig.Request.NoProgressBar
	kubeClient := mig.SourceInfo.ClusterClient.KubeClient
	jobName := attempt.HelmReleaseNamePrefix + "-rsync"

	if err = k8s.WaitForJobCompletion(attempt.Logger, kubeClient, namespace, jobName, showProgressBar); err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	return nil
}

func buildRsyncCmdMnt2(mig *migration.Migration) (string, error) {
	srcPath := srcMountPath + "/" + mig.Request.Source.Path
	destPath := destMountPath + "/" + mig.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:  mig.Request.NoChown,
		Delete:   mig.Request.DeleteExtraneousFiles,
		SrcPath:  srcPath,
		DestPath: destPath,
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

	if (sourceInfo.SupportsROX || sourceInfo.SupportsRWX) && destInfo.SupportsRWX {
		return ""
	}

	if !sourceInfo.SupportsROX && !sourceInfo.SupportsRWX {
		return sourceInfo.MountedNode
	}

	return destInfo.MountedNode
}
