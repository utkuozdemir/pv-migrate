package strategy

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/cli/values"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/narrate"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

const (
	mountStrategy        = "mount"
	clusterIPStrategy    = "clusterip"
	loadBalancerStrategy = "loadbalancer"
	localStrategy        = "local"
	nodePortStrategy     = "nodeport"

	srcMountPath  = "/source"
	destMountPath = "/dest"

	rsyncComponent = "rsync"
	sshdComponent  = "sshd"

	keyEnabled       = "enabled"
	keyNamespace     = "namespace"
	keyNetworkPolicy = "networkPolicy"
	keyPublicKey     = "publicKey"
	keyPVCMounts     = "pvcMounts"
	keyName          = "name"
	keyMountPath     = "mountPath"
	keyReadOnly      = "readOnly"
	keyAffinity      = "affinity"

	rootSSHUser    = "root"
	rootSSHPort    = 22
	nonRootSSHUser = "pvmigrate"
	nonRootSSHPort = 2222
	nonRootUID     = 10000
)

// Describe says in one line what a strategy does, for the step that announces
// the attempt. The direction matters for the ones that run sshd on one side.
func Describe(name string, push bool) string {
	side := "sshd next to the source, rsync next to the destination"
	if push {
		side = "sshd next to the destination, rsync next to the source"
	}

	switch name {
	case mountStrategy:
		return "one pod mounts both claims and copies locally, no network"
	case clusterIPStrategy:
		return side + ", over a ClusterIP service"
	case loadBalancerStrategy:
		return side + ", over a LoadBalancer service"
	case nodePortStrategy:
		return side + ", over a NodePort service"
	case localStrategy:
		return "sshd on both sides, the copy goes through this machine"
	default:
		return ""
	}
}

var (
	nameToStrategy = map[string]Strategy{
		mountStrategy:        &Mount{},
		clusterIPStrategy:    &ClusterIP{},
		loadBalancerStrategy: &LoadBalancer{},
		localStrategy:        &Local{},
		nodePortStrategy:     &NodePort{},
	}

	helmProviders = getter.All(cli.New())

	ErrUnaccepted = errors.New("unaccepted")
)

// DeclinedError reports that a strategy cannot handle this migration, together
// with the reason it gave. It unwraps to ErrUnaccepted so the ladder's existing
// check is unaffected, and the reason stays reachable without parsing a message.
type DeclinedError struct {
	Reason string
}

func (e *DeclinedError) Error() string {
	return e.Reason + ": " + ErrUnaccepted.Error()
}

func (e *DeclinedError) Unwrap() error {
	return ErrUnaccepted
}

// Declined returns the error a strategy returns when it cannot do the job.
func Declined(reason string) error {
	return &DeclinedError{Reason: reason}
}

type Strategy interface {
	// Run runs the migration for the given task execution.
	//
	// This is the actual implementation of the migration.
	Run(ctx context.Context, attempt *migration.Attempt, logger *slog.Logger) error
}

func GetStrategiesMapForNames(names []string) (map[string]Strategy, error) {
	sts := make(map[string]Strategy)

	for _, name := range names {
		s, ok := nameToStrategy[name]
		if !ok {
			return nil, fmt.Errorf("strategy not found: %s", name)
		}

		sts[name] = s
	}

	return sts, nil
}

func Cleanup(attempt *migration.Attempt, logger *slog.Logger) error {
	mig := attempt.Migration
	req := mig.Request

	details := narrate.Detail(logger, 1)

	var errs error

	// Every release is tried on both sides, since the attempt does not record
	// which cluster each one went to. The side that never had it says so
	// quietly.
	for _, info := range []*pvc.Info{mig.SourceInfo, mig.DestInfo} {
		for _, name := range attempt.ReleaseNames {
			removed, err := cleanupForPVC(name, req.HelmTimeout, info)
			if err != nil {
				errs = multierror.Append(errs, err)

				continue
			}

			if removed {
				details.Info(fmt.Sprintf("🧹 removed release %s from namespace %s", name, info.Claim.Namespace))
			}
		}
	}

	return errs
}

// cleanupForPVC uninstalls the release from the claim's cluster, and reports
// whether there was one to remove.
func cleanupForPVC(helmReleaseName string, helmUninstallTimeout time.Duration, pvcInfo *pvc.Info) (bool, error) {
	ac, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return false, err
	}

	uninstall := action.NewUninstall(ac)
	uninstall.WaitStrategy = kube.LegacyStrategy
	uninstall.Timeout = helmUninstallTimeout

	_, err = uninstall.Run(helmReleaseName)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, driver.ErrReleaseNotFound) || apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, fmt.Errorf("failed to uninstall helm release %s: %w", helmReleaseName, err)
}

func initHelmActionConfig(pvcInfo *pvc.Info) (*action.Configuration, error) {
	actionConfig := new(action.Configuration)

	err := actionConfig.Init(pvcInfo.ClusterClient.RESTClientGetter,
		pvcInfo.Claim.Namespace, os.Getenv("HELM_DRIVER"))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	return actionConfig, nil
}

func sshUser(req *migration.Request) string {
	if req.NonRoot {
		return nonRootSSHUser
	}

	return rootSSHUser
}

func sshPort(req *migration.Request) int {
	if req.NonRoot {
		return nonRootSSHPort
	}

	return rootSSHPort
}

func applyNonRootValues(vals map[string]any, req *migration.Request) {
	if !req.NonRoot {
		return
	}

	nonRootSecCtx := map[string]any{
		"runAsNonRoot":             true,
		"runAsUser":                nonRootUID,
		"runAsGroup":               nonRootUID,
		"allowPrivilegeEscalation": false,
	}
	nonRootPodSecCtx := map[string]any{
		"fsGroup": nonRootUID,
	}

	for _, component := range []string{sshdComponent, rsyncComponent} {
		section, ok := vals[component].(map[string]any)
		if !ok {
			continue
		}

		section["securityContext"] = nonRootSecCtx
		section["podSecurityContext"] = nonRootPodSecCtx
	}

	if sshd, ok := vals["sshd"].(map[string]any); ok {
		sshd["containerPort"] = nonRootSSHPort
		sshd["publicKeyMountPath"] = "/home/pvmigrate/.ssh/authorized_keys"
	}
}

func getMergedHelmValues(
	baseValues map[string]any,
	request *migration.Request,
) (map[string]any, error) {
	// If an image tag is set, inject it as the lowest-priority --set values
	// so user overrides via --helm-set take precedence.
	helmValues := request.HelmValues
	if tag := request.ImageTag; tag != "" {
		imageTagValues := []string{
			"rsync.image.tag=" + tag,
			"sshd.image.tag=" + tag,
		}
		merged := make([]string, 0, len(imageTagValues)+len(helmValues))
		merged = append(merged, imageTagValues...)
		helmValues = append(merged, helmValues...)
	}

	valsOptions := values.Options{
		ValueFiles:   request.HelmValuesFiles,
		Values:       helmValues,
		StringValues: request.HelmStringValues,
		FileValues:   request.HelmFileValues,
	}

	userValues, err := valsOptions.MergeValues(helmProviders)
	if err != nil {
		return nil, fmt.Errorf("failed to merge helm values: %w", err)
	}

	// Merge using Helm's own MergeMaps: user values override base values.
	return loader.MergeMaps(baseValues, userValues), nil
}

func installHelmChart(
	ctx context.Context,
	attempt *migration.Attempt,
	pvcInfo *pvc.Info,
	name string,
	values map[string]any,
	logger *slog.Logger,
) error {
	// Recorded before anything is installed, and here rather than reconstructed
	// later, because this is the one point every strategy passes through and the
	// only one that knows which cluster and namespace this release goes into. An
	// install that creates nothing simply yields nothing to report.
	attempt.DiagnosticTargets = append(attempt.DiagnosticTargets,
		migration.DiagnosticTarget{Release: name, Info: pvcInfo})

	helmActionConfig, err := initHelmActionConfig(pvcInfo)
	if err != nil {
		return fmt.Errorf("failed to init helm action config: %w", err)
	}

	mig := attempt.Migration

	install := action.NewInstall(helmActionConfig)
	install.Namespace = pvcInfo.Claim.Namespace
	install.ReleaseName = name
	timeout := mig.Request.HelmTimeout
	install.Timeout = timeout

	applyNonRootValues(values, mig.Request)

	// Before the user's values are merged on top, so that an explicit request
	// for a policy is still honored, and the install then reports the real
	// permission problem.
	helm.DisableNetworkPoliciesWhereForbidden(ctx, values, canCreateNetworkPolicies(pvcInfo), narrate.Detail(logger, 1))

	vals, err := getMergedHelmValues(values, mig.Request)
	if err != nil {
		return fmt.Errorf("failed to get merged helm values: %w", err)
	}

	// Helm's wait blocks on a LoadBalancer Service until it has an address, and
	// on a cluster without a load balancer controller that is forever. Such a
	// release is not waited on by Helm at all. The sshd pod is waited for below
	// instead, with the same budget, since nothing else would, and the strategy
	// waits for the address itself, with its own budget and a fallback. Decided
	// on the merged values, since the user's values can change the Service type.
	install.WaitStrategy = kube.LegacyStrategy
	waitForSshd := installsLoadBalancer(vals)

	if waitForSshd {
		install.WaitStrategy = kube.HookOnlyStrategy
	}

	// A long wait usually means the cluster already knows what is stuck, so at
	// half the budget the resources are peeked at once, turning the silent half
	// of the wait into an answer.
	stopPeek := peekAfter(timeout/2, func() {
		writeMidInstallDiagnostics(ctx, attempt, pvcInfo, name, logger)
	})
	defer stopPeek()

	if _, err = install.Run(mig.Chart, vals); err != nil {
		// The bare context error names no duration and no knob, and it is the
		// headline of every stuck-resource failure.
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf(
				"timed out after %s waiting for the release's resources to become ready (see --helm-timeout): %w",
				timeout, err)
		}

		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	return describeRelease(ctx, pvcInfo.ClusterClient.KubeClient, name, vals, timeout, logger)
}

// describeRelease tells what the release put in the cluster, with the names a
// reader can look up, and waits for the sshd pod on the way: for a release Helm
// was told not to wait for that is the readiness gate, and for the others the
// pod is ready already and the wait returns at once.
func describeRelease(
	ctx context.Context,
	cli kubernetes.Interface,
	release string,
	vals map[string]any,
	timeout time.Duration,
	logger *slog.Logger,
) error {
	details := narrate.Detail(logger, 1)
	deeper := narrate.Detail(logger, 2)

	details.Info("📦 created release " + release)

	if sshd, ok := helm.EnabledComponent(vals, sshdComponent); ok {
		namespace := helm.ComponentNamespace(sshd)

		pod, err := k8s.WaitForPodReady(ctx, cli, namespace, sshdLabelSelector(release), timeout, deeper)
		if err != nil {
			return fmt.Errorf("failed to wait for the sshd pod to become ready (see --helm-timeout): %w", err)
		}

		deeper.Info(fmt.Sprintf("🏃 sshd pod %s in namespace %s, on node %s, image %s, %s",
			pod.Name, namespace, pod.Spec.NodeName, podImage(pod), helm.DescribeMounts(sshd)))
		describeService(ctx, cli, namespace, release+"-sshd", deeper)
		describeNetworkPolicy(sshd, "sshd", deeper)
	}

	if rsync, ok := helm.EnabledComponent(vals, rsyncComponent); ok {
		namespace := helm.ComponentNamespace(rsync)

		deeper.Info(fmt.Sprintf("🚚 rsync job %s-rsync in namespace %s, image %s, %s",
			release, namespace, k8s.JobImage(ctx, cli, namespace, release+"-rsync"), helm.DescribeMounts(rsync)))
		describeNetworkPolicy(rsync, "rsync", deeper)
	}

	return nil
}

// podImage is the image the pod's first container runs, which is the data mover.
func podImage(pod *corev1.Pod) string {
	if len(pod.Spec.Containers) == 0 {
		return "unknown"
	}

	return pod.Spec.Containers[0].Image
}

// narrateConnection says which side rsync connects to and where, under the
// release it belongs to.
func narrateConnection(logger *slog.Logger, push bool, host string, port int) {
	verb := "pulls from"
	if push {
		verb = "pushes to"
	}

	address := host
	if port != 0 {
		address = fmt.Sprintf("%s:%d", host, port)
	}

	narrate.Detail(logger, 2).Info(fmt.Sprintf("🔗 %s sshd at %s", verb, address))
}

// describeService tells how the sshd Service can be reached, from the object
// the cluster created rather than from what was asked for.
func describeService(ctx context.Context, cli kubernetes.Interface, namespace, name string, logger *slog.Logger) {
	svc, err := cli.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || len(svc.Spec.Ports) == 0 {
		return
	}

	port := svc.Spec.Ports[0]
	line := fmt.Sprintf("🔌 service %s, type %s, cluster address %s:%d",
		name, svc.Spec.Type, svc.Spec.ClusterIP, port.Port)

	if port.NodePort != 0 {
		line += fmt.Sprintf(", node port %d", port.NodePort)
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		line += ", load balancer address pending"

		if ingress := svc.Status.LoadBalancer.Ingress; len(ingress) > 0 {
			line = strings.TrimSuffix(line, " pending") + " " + cmp.Or(ingress[0].Hostname, ingress[0].IP)
		}
	}

	logger.Info(line)
}

// describeNetworkPolicy says whether the component's pod got its allow-all
// policy. The denied case has its own warning by then.
func describeNetworkPolicy(section map[string]any, component string, logger *slog.Logger) {
	if !helm.NetworkPolicyOn(section) {
		return
	}

	logger.Info(
		fmt.Sprintf("🔒 network policy for the %s pod, so a default-deny namespace does not block it", component),
	)
}

// canCreateNetworkPolicies asks the cluster the release goes into.
func canCreateNetworkPolicies(pvcInfo *pvc.Info) helm.CanCreateNetworkPoliciesFunc {
	return func(ctx context.Context, namespace string) (bool, error) {
		return k8s.CanCreateNetworkPolicies(ctx, pvcInfo.ClusterClient.KubeClient, namespace)
	}
}

// installsLoadBalancer reports whether the values put an sshd with a
// LoadBalancer Service into the release, whose address Helm's wait would
// otherwise block on. A release without sshd renders no Service, whatever the
// values say about its type.
func installsLoadBalancer(values map[string]any) bool {
	sshd, ok := values[sshdComponent].(map[string]any)
	if !ok || sshd[keyEnabled] != true {
		return false
	}

	service, ok := sshd["service"].(map[string]any)
	if !ok {
		return false
	}

	return service["type"] == "LoadBalancer"
}

// peekAfter runs the peek once after the delay unless stopped first.
func peekAfter(delay time.Duration, peek func()) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-done:
		case <-time.After(delay):
			peek()
		}
	}()

	return func() { close(done) }
}

// writeMidInstallDiagnostics narrates a still-running install wait with what the
// cluster reports at that moment, on the same writer and in the same shape the
// failure block would use. Log records carry it on a structured stream.
func writeMidInstallDiagnostics(
	ctx context.Context,
	attempt *migration.Attempt,
	pvcInfo *pvc.Info,
	release string,
	logger *slog.Logger,
) {
	req := attempt.Migration.Request
	cli := pvcInfo.ClusterClient.KubeClient
	ns := pvcInfo.Claim.Namespace

	if req.StructuredLogs {
		var buf bytes.Buffer

		k8s.WriteWorkloadDiagnostics(ctx, cli, ns,
			k8s.InstanceLabelSelector(release), console.Palette{}, &buf, logger)
		logger.Warn("🔶 Still waiting for the release's resources. What the cluster reports so far",
			"release", release, "namespace", ns, "diagnostics", buf.String())

		return
	}

	palette := console.Palette{Enabled: req.ColorOutput}

	fmt.Fprintf(req.Writer, "\n%s\n\n  %s (namespace %s):\n",
		palette.Bold("Still waiting. What the cluster reports so far:"), release, ns)
	k8s.WriteWorkloadDiagnostics(ctx, cli, ns,
		k8s.InstanceLabelSelector(release), palette, req.Writer, logger)
	// The wait goes on narrating under its step after this, so the block closes
	// with a blank line rather than letting a detail follow the diagnostics directly.
	fmt.Fprintln(req.Writer)
}
