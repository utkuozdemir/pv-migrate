package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/migration"
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

func (r *Mnt2) Run(attempt *migration.Attempt) (bool, error) {
	migration := attempt.Migration
	if !r.canDo(migration) {
		return false, nil
	}

	sourceInfo := attempt.Migration.SourceInfo
	destInfo := attempt.Migration.DestInfo
	namespace := sourceInfo.Claim.Namespace

	node := determineTargetNode(migration)

	srcMountPath := "/source"
	destMountPath := "/dest"

	srcPath := srcMountPath + "/" + migration.Request.Source.Path
	destPath := destMountPath + "/" + migration.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:  migration.Request.NoChown,
		Delete:   migration.Request.DeleteExtraneousFiles,
		SrcPath:  srcPath,
		DestPath: destPath,
	}
	rsyncCmdStr, err := rsyncCmd.Build()
	if err != nil {
		return true, err
	}

	vals := map[string]interface{}{
		"rsync": map[string]interface{}{
			"enabled":   true,
			"namespace": namespace,
			"nodeName":  node,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      sourceInfo.Claim.Name,
					"mountPath": srcMountPath,
					"readOnly":  migration.Request.SourceMountReadOnly,
				},
				{
					"name":      destInfo.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command": rsyncCmdStr,
		},
	}

	releaseName := attempt.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	doneCh := registerCleanupHook(attempt, releaseNames)
	defer cleanupAndReleaseHook(attempt, releaseNames, doneCh)

	err = installHelmChart(attempt, sourceInfo, releaseName, vals)
	if err != nil {
		return true, err
	}

	showProgressBar := !migration.Request.NoProgressBar
	kubeClient := migration.SourceInfo.ClusterClient.KubeClient
	jobName := attempt.HelmReleaseNamePrefix + "-rsync"
	err = k8s.WaitForJobCompletion(attempt.Logger, kubeClient, namespace, jobName, showProgressBar)

	return true, err
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
