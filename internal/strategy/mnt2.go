package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/migration"
)

type Mnt2 struct{}

func (r *Mnt2) canDo(t *migration.Migration) bool {
	s := t.SourceInfo
	d := t.DestInfo
	sameCluster := s.ClusterClient.RestConfig.Host == d.ClusterClient.RestConfig.Host
	if !sameCluster {
		return false
	}

	sameNamespace := s.Claim.Namespace == d.Claim.Namespace
	if !sameNamespace {
		return false
	}

	sameNode := s.MountedNode == d.MountedNode

	return sameNode || s.SupportsROX || s.SupportsRWX || d.SupportsRWX
}

func (r *Mnt2) Run(a *migration.Attempt) (bool, error) {
	m := a.Migration
	if !r.canDo(m) {
		return false, nil
	}

	s := a.Migration.SourceInfo
	d := a.Migration.DestInfo
	ns := s.Claim.Namespace

	node := determineTargetNode(m)

	srcMountPath := "/source"
	destMountPath := "/dest"

	srcPath := srcMountPath + "/" + m.Request.Source.Path
	destPath := destMountPath + "/" + m.Request.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:  m.Request.NoChown,
		Delete:   m.Request.DeleteExtraneousFiles,
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
			"namespace": ns,
			"nodeName":  node,
			"pvcMounts": []map[string]interface{}{
				{
					"name":      s.Claim.Name,
					"mountPath": srcMountPath,
					"readOnly":  m.Request.SourceMountReadOnly,
				},
				{
					"name":      d.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command": rsyncCmdStr,
		},
	}

	releaseName := a.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	doneCh := registerCleanupHook(a, releaseNames)
	defer cleanupAndReleaseHook(a, releaseNames, doneCh)

	err = installHelmChart(a, s, releaseName, vals)
	if err != nil {
		return true, err
	}

	showProgressBar := !m.Request.NoProgressBar
	kubeClient := m.SourceInfo.ClusterClient.KubeClient
	jobName := a.HelmReleaseNamePrefix + "-rsync"
	err = k8s.WaitForJobCompletion(a.Logger, kubeClient, ns, jobName, showProgressBar)

	return true, err
}

func determineTargetNode(t *migration.Migration) string {
	s := t.SourceInfo
	d := t.DestInfo
	if (s.SupportsROX || s.SupportsRWX) && d.SupportsRWX {
		return ""
	}
	if !s.SupportsROX && !s.SupportsRWX {
		return s.MountedNode
	}

	return d.MountedNode
}
