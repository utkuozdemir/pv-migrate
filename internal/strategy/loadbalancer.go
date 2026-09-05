package strategy

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
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

	// The address wait retries a failing read until its deadline and then reports
	// a timeout, whatever the cause was. Reading the Service once first makes an
	// API problem fail here with its own message, so that only a missing address
	// falls back.
	cli := info.ClusterClient.KubeClient

	if _, err := cli.CoreV1().Services(info.Claim.Namespace).Get(ctx, svcName, metav1.GetOptions{}); err != nil {
		return sshTarget{}, fmt.Errorf("failed to get service %s/%s: %w", info.Claim.Namespace, svcName, err)
	}

	details := narrate.Detail(logger, 1)
	details.Info(fmt.Sprintf("⏳ waiting up to %s for the load balancer address", req.LoadBalancerTimeout))

	lbAddress, err := k8s.GetServiceAddress(ctx, cli, info.Claim.Namespace, svcName, req.LoadBalancerTimeout)
	if err == nil {
		details.Info("🔗 load balancer address " + lbAddress)

		return sshTarget{host: formatSSHTargetHost(lbAddress)}, nil
	}

	// An interrupted run is not a pending load balancer.
	if ctx.Err() != nil {
		return sshTarget{}, err
	}

	details.Warn("🔶 no load balancer address within --loadbalancer-timeout, using the Service's node port instead. " +
		"Expected on a cluster without a load balancer controller. If yours has one and is slow, raise the timeout")

	target, fallbackErr := resolveNodePortTarget(ctx, attempt, topo, sshdRelease, logger)
	if fallbackErr != nil {
		return sshTarget{}, fmt.Errorf("%w, and the node port fallback failed: %w", err, fallbackErr)
	}

	return target, nil
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
