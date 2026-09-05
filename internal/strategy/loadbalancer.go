package strategy

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/util"
)

type LoadBalancer struct{}

func (r *LoadBalancer) Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error {
	return runTwoReleaseStrategy(ctx, attempt, "LoadBalancer", resolveLBTarget, logger)
}

// resolveLBTarget waits for the load balancer's address and, when none arrives
// within the budget, falls back to the node port the same Service carries. A
// cluster without a load balancer controller leaves the Service pending
// forever, and before this fallback that was the end of the default order.
func resolveLBTarget(
	ctx context.Context,
	attempt *migration.Attempt,
	topo topology,
	sshdRelease string,
	logger *slog.Logger,
) (sshTarget, error) {
	info := topo.sshd.info
	req := attempt.Migration.Request
	svcName := sshdRelease + "-sshd"

	if err := waitForSshdReady(ctx, attempt, info, sshdRelease, logger); err != nil {
		return sshTarget{}, err
	}

	// The address wait retries a failing read until its deadline and then reports
	// a timeout, whatever the cause was. Reading the Service once first makes an
	// API problem fail here with its own message, so that only a missing address
	// falls back.
	cli := info.ClusterClient.KubeClient

	if _, err := cli.CoreV1().Services(info.Claim.Namespace).Get(ctx, svcName, metav1.GetOptions{}); err != nil {
		return sshTarget{}, fmt.Errorf("failed to get service %s/%s: %w", info.Claim.Namespace, svcName, err)
	}

	logger.Info("⏳ Waiting for the load balancer address", "timeout", req.LoadBalancerTimeout)

	lbAddress, err := k8s.GetServiceAddress(ctx, cli, info.Claim.Namespace, svcName, req.LoadBalancerTimeout)
	if err == nil {
		return sshTarget{host: formatSSHTargetHost(lbAddress)}, nil
	}

	// An interrupted run is not a pending load balancer.
	if ctx.Err() != nil {
		return sshTarget{}, fmt.Errorf("failed to get service address: %w", err)
	}

	logger.Warn("🔶 No load balancer address within --loadbalancer-timeout, falling back to the Service's node port. "+
		"This is expected on a cluster without a load balancer controller. "+
		"If yours has one and is just slow, raise the timeout",
		"timeout", req.LoadBalancerTimeout, "error", err)

	target, fallbackErr := resolveNodePortTarget(ctx, attempt, topo, sshdRelease, logger)
	if fallbackErr != nil {
		return sshTarget{}, fmt.Errorf("failed to get service address: %w, and the node port fallback failed: %w",
			err, fallbackErr)
	}

	return target, nil
}

// waitForSshdReady stands in for Helm's wait on the sshd release, which is
// switched off for a release with a LoadBalancer Service because it would block
// on the address (see installHelmChart). It waits for the sshd pod to become
// ready within the same budget Helm would have had, and looks at what the
// cluster reports halfway through, the same way the install wait does.
func waitForSshdReady(
	ctx context.Context,
	attempt *migration.Attempt,
	info *pvc.Info,
	sshdRelease string,
	logger *slog.Logger,
) error {
	timeout := attempt.Migration.Request.HelmTimeout

	stopPeek := peekAfter(timeout/2, func() {
		writeMidInstallDiagnostics(ctx, attempt, info, sshdRelease, logger)
	})
	defer stopPeek()

	_, err := k8s.WaitForPodReady(ctx, info.ClusterClient.KubeClient, info.Claim.Namespace,
		sshdLabelSelector(sshdRelease), timeout, logger)
	if err != nil {
		return fmt.Errorf("failed to wait for the sshd pod to become ready (see --helm-timeout): %w", err)
	}

	return nil
}

// formatSSHTargetHost renders a host for use in rsync's `[user@]host:path`
// remote spec, which is split on the first colon, so an IPv6 literal has to be
// bracketed or rsync reads only its first group as the host. It is idempotent:
// an already-bracketed literal does not parse as an address and is returned
// unchanged, which is what lets it also be applied to --dest-host-override.
func formatSSHTargetHost(host string) string {
	if util.IsIPv6(host) {
		return fmt.Sprintf("[%s]", host)
	}

	return host
}
