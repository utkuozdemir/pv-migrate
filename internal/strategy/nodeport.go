package strategy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
)

type NodePort struct{}

func (r *NodePort) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	return runTwoReleaseStrategy(ctx, attempt, "NodePort", resolveNodePortTarget, logger)
}

// resolveNodePortTarget pairs the Service's node port with the address of the
// node the sshd pod runs on, so the connection lands on the node that has the
// pod rather than on one that has to forward it. Any node works when that one
// has no usable address. With a host override the user has said where to
// connect, so no node is looked up at all, which also spares the permission to
// read nodes.
func resolveNodePortTarget(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	sshdRelease string,
	logger *slog.Logger,
) (sshTarget, error) {
	info := topo.sshd.info
	cli := info.ClusterClient.KubeClient
	svcName := sshdRelease + "-sshd"

	nodePort, err := k8s.GetNodePort(
		ctx,
		cli,
		info.Claim.Namespace,
		svcName,
		attempt.Migration.Request.LoadBalancerTimeout,
	)
	if err != nil {
		return sshTarget{}, fmt.Errorf("failed to get NodePort: %w", err)
	}

	details := narrate.Detail(logger, 1)

	if attempt.Migration.Request.DestHostOverride != "" {
		details.Info(fmt.Sprintf("🔗 connecting to node port %d on the host override", nodePort))

		// The caller fills the host in from the override, so only the port is
		// resolved here.
		return sshTarget{port: nodePort}, nil
	}

	sshdPod, err := getSshdPodForHelmRelease(ctx, info, sshdRelease, details)
	if err != nil {
		return sshTarget{}, fmt.Errorf("failed to get sshd pod: %w", err)
	}

	podNode := sshdPod.Spec.NodeName

	nodeIP, err := k8s.GetNodeIP(ctx, cli, podNode)
	if err != nil {
		details.Warn(fmt.Sprintf("🔶 could not get the address of node %s, using another node: %v", podNode, err))

		nodeIP, err = k8s.GetAnyNodeIP(ctx, cli)
		if err != nil {
			return sshTarget{}, fmt.Errorf("failed to find usable node IP: %w", err)
		}
	} else {
		details.Info(fmt.Sprintf("🔗 connecting to node port %d on node %s at %s, where the sshd pod runs",
			nodePort, podNode, nodeIP))
	}

	return sshTarget{host: formatSSHTargetHost(nodeIP), port: nodePort}, nil
}
