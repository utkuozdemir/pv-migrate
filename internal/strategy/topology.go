package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/ssh"
)

// topology maps sshd and rsync components to source/dest sides.
// In pull mode (default), sshd runs on the source side and rsync on the dest side.
// In push mode (--rsync-push), sshd runs on the dest side and rsync on the source side.
type topology struct {
	sshd  componentSide
	rsync componentSide
	push  bool
}

// componentSide describes which cluster/PVC a component is deployed on.
type componentSide struct {
	info      *pvc.Info
	mountPath string
	readOnly  bool
}

// sshTarget holds the resolved SSH connection endpoint for two-release strategies.
type sshTarget struct {
	host string
	port int
}

func resolveTopology(mig *migration.Migration) topology {
	src := mig.SourceInfo
	dst := mig.DestInfo
	srcReadOnly := !mig.Request.SourceMountReadWrite

	if mig.Request.Push {
		return topology{
			push:  true,
			sshd:  componentSide{info: dst, mountPath: destMountPath, readOnly: false},
			rsync: componentSide{info: src, mountPath: srcMountPath, readOnly: srcReadOnly},
		}
	}

	return topology{
		push:  false,
		sshd:  componentSide{info: src, mountPath: srcMountPath, readOnly: srcReadOnly},
		rsync: componentSide{info: dst, mountPath: destMountPath, readOnly: false},
	}
}

// releaseNames returns sshd and rsync Helm release names for two-release strategies.
// The suffix reflects which cluster each release is installed on (-src or -dest).
func (t topology) releaseNames(prefix string) [2]string {
	if t.push {
		return [2]string{prefix + "-dest", prefix + "-src"}
	}

	return [2]string{prefix + "-src", prefix + "-dest"}
}

func generateSSHKeys(keyAlgorithm string, logger *slog.Logger) (string, string, string, error) {
	logger.Info("🔑 Generating SSH key pair", "algorithm", keyAlgorithm)

	publicKey, privateKey, err := ssh.CreateSSHKeyPair(keyAlgorithm)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create ssh key pair: %w", err)
	}

	return publicKey, privateKey, "/tmp/id_" + keyAlgorithm, nil
}

type resolveTargetFunc func(ctx context.Context, attempt *migration.Attempt,
	topo topology, sshdRelease string, _ *slog.Logger) (sshTarget, error)

// runTwoReleaseStrategy runs a two-release (sshd + rsync) migration.
// The resolveTarget callback is called after sshd is installed to determine the SSH endpoint.
func runTwoReleaseStrategy(
	ctx context.Context,
	attempt *migration.Attempt,
	serviceType string,
	resolveTarget resolveTargetFunc,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	topo := resolveTopology(mig)

	publicKey, privateKey, privateKeyMountPath, err := generateSSHKeys(mig.Request.KeyAlgorithm, logger)
	if err != nil {
		return err
	}

	releases := topo.releaseNames(attempt.HelmReleaseNamePrefix)
	sshdRelease, rsyncRelease := releases[0], releases[1]
	attempt.ReleaseNames = releases[:]

	if err = installSshd(ctx, attempt, topo, sshdRelease, publicKey, serviceType, logger); err != nil {
		return fmt.Errorf("failed to install sshd: %w", err)
	}

	target, err := resolveTarget(ctx, attempt, topo, sshdRelease, logger)
	if err != nil {
		return err
	}

	sshTargetHost := target.host
	if mig.Request.DestHostOverride != "" {
		sshTargetHost = formatSSHTargetHost(mig.Request.DestHostOverride)
	}

	if err = installRsyncJob(ctx, attempt, topo, rsyncRelease, privateKey, privateKeyMountPath,
		sshTargetHost, target.port, logger); err != nil {
		return fmt.Errorf("failed to install rsync job: %w", err)
	}

	return waitForRsyncJob(ctx, attempt, topo.rsync.info, rsyncRelease, logger)
}

// buildRsyncCmdString resolves the PVC paths and returns the rsync command to run
// on whichever side the topology puts it.
func buildRsyncCmdString(req *migration.Request, push bool, sshHost string, port int) (string, error) {
	srcPath, destPath, err := resolveMountPaths(req)
	if err != nil {
		return "", err
	}

	cmd := rsync.Cmd{
		Port:      port,
		NoChown:   req.NoChown,
		NonRoot:   req.NonRoot,
		Delete:    req.DeleteExtraneousFiles,
		SrcPath:   srcPath,
		DestPath:  destPath,
		Compress:  !req.NoCompress,
		ExtraArgs: req.RsyncExtraArgs,
	}

	if push {
		cmd.DestUseSSH = true
		cmd.DestSSHHost = sshHost
		cmd.DestSSHUser = sshUser(req)
	} else {
		cmd.SrcUseSSH = true
		cmd.SrcSSHHost = sshHost
		cmd.SrcSSHUser = sshUser(req)
	}

	built, err := cmd.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build rsync command: %w", err)
	}

	return built, nil
}

func buildSshdHelmValues(side componentSide, publicKey string) map[string]any {
	return map[string]any{
		keyEnabled:   true,
		keyNamespace: side.info.Claim.Namespace,
		keyPublicKey: publicKey,
		keyPVCMounts: []map[string]any{
			{
				keyName:      side.info.Claim.Name,
				keyReadOnly:  side.readOnly,
				keyMountPath: side.mountPath,
			},
		},
		keyAffinity: side.info.AffinityHelmValues,
	}
}

func buildRsyncHelmValues(side componentSide, rsyncCmd, privateKey, privateKeyMountPath string) map[string]any {
	return map[string]any{
		keyEnabled:            true,
		keyNamespace:          side.info.Claim.Namespace,
		"privateKeyMount":     true,
		"privateKey":          privateKey,
		"privateKeyMountPath": privateKeyMountPath,
		keyPVCMounts: []map[string]any{
			{
				keyName:      side.info.Claim.Name,
				keyMountPath: side.mountPath,
				keyReadOnly:  side.readOnly,
			},
		},
		"command":   rsyncCmd,
		keyAffinity: side.info.AffinityHelmValues,
	}
}

func installSshd(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	releaseName, publicKey, serviceType string,
	logger *slog.Logger,
) error {
	return installHelmChart(ctx, attempt, topo.sshd.info, releaseName,
		sshdReleaseValues(topo.sshd, publicKey, serviceType), logger)
}

// sshdReleaseValues builds the values of an sshd release whose Service has the
// given type. The type is what decides whether Helm waits for the release, see
// installsLoadBalancer.
func sshdReleaseValues(side componentSide, publicKey, serviceType string) map[string]any {
	sshdVals := buildSshdHelmValues(side, publicKey)
	sshdVals["service"] = map[string]any{"type": serviceType}

	return map[string]any{sshdComponent: sshdVals}
}

func installRsyncJob(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	releaseName, privateKey, privateKeyMountPath, sshHost string,
	sshPort int,
	logger *slog.Logger,
) error {
	mig := attempt.Migration

	rsyncCmdStr, err := buildRsyncCmdString(mig.Request, topo.push, sshHost, sshPort)
	if err != nil {
		return err
	}

	rsyncVals := buildRsyncHelmValues(topo.rsync, rsyncCmdStr, privateKey, privateKeyMountPath)
	rsyncVals["sshRemoteHost"] = sshHost

	if sshPort != 0 {
		rsyncVals["sshRemotePort"] = sshPort
	}

	return installHelmChart(
		ctx, attempt, topo.rsync.info, releaseName, map[string]any{rsyncComponent: rsyncVals}, logger)
}

func waitForRsyncJob(
	ctx context.Context,
	attempt *migration.Attempt,
	rsyncInfo *pvc.Info,
	rsyncRelease string,
	logger *slog.Logger,
) error {
	mig := attempt.Migration
	kubeClient := rsyncInfo.ClusterClient.KubeClient
	namespace := rsyncInfo.Claim.Namespace
	jobName := rsyncRelease + "-rsync"

	if mig.Request.Detach {
		if _, err := k8s.WaitForJobStart(ctx, kubeClient, namespace, jobName, logger); err != nil {
			return fmt.Errorf("failed to wait for job to start: %w", err)
		}

		attempt.Detached = true

		return nil
	}

	// Deliberately not wrapped: the error already names the failed pod and the
	// exit state, and the summary row already names the strategy, so a
	// "failed to wait for job completion" prefix would only push the answer
	// further right.
	//nolint:wrapcheck
	return k8s.WaitForJobCompletion(
		ctx, kubeClient, namespace, jobName,
		mig.Request.ShowProgressBar, mig.Request.StructuredLogs,
		console.Palette{Enabled: mig.Request.ColorOutput}, mig.Request.Writer, logger,
	)
}
