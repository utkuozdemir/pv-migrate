package strategy

import (
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

type Mnt2 struct {
}

func (r *Mnt2) canDo(t *task.Task) bool {
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

func (r *Mnt2) Run(e *task.Execution) (bool, error) {
	t := e.Task
	if !r.canDo(t) {
		return false, nil
	}

	s := e.Task.SourceInfo
	d := e.Task.DestInfo
	ns := s.Claim.Namespace
	opts := t.Migration.Options

	node := determineTargetNode(t)

	srcMountPath := "/source"
	destMountPath := "/dest"

	srcPath := srcMountPath + "/" + t.Migration.Source.Path
	destPath := destMountPath + "/" + t.Migration.Dest.Path

	rsyncCmd := rsync.Cmd{
		NoChown:  opts.NoChown,
		Delete:   opts.DeleteExtraneousFiles,
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
					"readOnly":  opts.SourceMountReadOnly,
				},
				{
					"name":      d.Claim.Name,
					"mountPath": destMountPath,
				},
			},
			"command": rsyncCmdStr,
		},
	}

	releaseName := e.HelmReleaseNamePrefix
	releaseNames := []string{releaseName}

	doneCh := registerCleanupHook(e, releaseNames)
	defer cleanupAndReleaseHook(e, releaseNames, doneCh)

	err = installHelmChart(e, s, releaseName, vals)
	if err != nil {
		return true, err
	}

	showProgressBar := !opts.NoProgressBar
	kubeClient := t.SourceInfo.ClusterClient.KubeClient
	jobName := e.HelmReleaseNamePrefix + "-rsync"
	err = k8s.WaitForJobCompletion(e.Logger, kubeClient, ns, jobName, showProgressBar)
	return true, err
}

func determineTargetNode(t *task.Task) string {
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
