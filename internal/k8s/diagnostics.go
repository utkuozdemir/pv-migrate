package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/utkuozdemir/pv-migrate/internal/console"
)

const (
	instanceLabel = "app.kubernetes.io/instance"

	// diagnosticsTimeout bounds the whole block. This runs on the failure path of
	// an operation that is already over, so it may not add a wait of its own.
	diagnosticsTimeout = 15 * time.Second

	// maxEventsPerObject caps what is printed per object. The no-truncation rule
	// is right for an error message and wrong for an event list.
	maxEventsPerObject = 3

	// maxEventsList caps the namespace-wide fetch the per-object cap selects from.
	maxEventsList = 500
)

// InstanceLabelSelector selects everything a Helm release created, through the
// label the chart puts on every resource.
func InstanceLabelSelector(release string) string {
	return instanceLabel + "=" + release
}

// workloadFacts is what the cluster said about one object.
type workloadFacts struct {
	uid     types.UID
	kind    string
	name    string
	title   string
	details []string
	// quiet marks a fact that repeats what its pods already say, kept only when
	// nothing else tells the story. bareTitle is what to print then: counters
	// that lag their own pod would read as a contradiction next to the event
	// the row was kept for.
	quiet     bool
	bareTitle string
}

// WriteWorkloadDiagnostics writes what the cluster reports about the resources of
// one Helm release: the workloads, their pods' container states, their services'
// addresses, and the warning events attached to any of them. Every line is an
// observation, none is a conclusion. It never fails the caller: a request that is
// forbidden or times out drops only what it would have added.
func WriteWorkloadDiagnostics(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, labelSelector string,
	palette console.Palette,
	writer io.Writer,
	logger *slog.Logger,
) {
	if cli == nil || writer == nil {
		return
	}

	ctx, cancel := detachedContext(ctx)
	defer cancel()

	objects, listedAny := listWorkloads(ctx, cli, ns, labelSelector, palette, logger)

	// An empty result is only the observation "nothing exists" when something
	// was actually read. Refused or failed lists reported as absence would tell
	// a user on a locked-down cluster that the release created nothing.
	if !listedAny {
		fmt.Fprintln(writer, "    "+palette.Warn("could not read the cluster's resources"))

		return
	}

	if len(objects) == 0 {
		fmt.Fprintln(writer, "    "+palette.Dim("no resources found"))

		return
	}

	events := warningEventsByUID(ctx, cli, ns, logger)

	// A job row saying "1 active" under its own freshly failed pod reads as a
	// contradiction: the controller's counters lag the pod. The row earns its
	// place only when it carries something its pods do not.
	objects = dropShadowedQuietFacts(objects, events)

	for _, object := range objects {
		fmt.Fprintf(writer, "    %s\n", object.title)

		for _, detail := range object.details {
			fmt.Fprintf(writer, "      %s\n", detail)
		}

		for _, event := range events[object.uid] {
			fmt.Fprintf(writer, "      %s\n", palette.Warn(event))
		}
	}
}

// detachedContext bounds the diagnostics work while surviving the parent's
// cancellation: the failure being explained may be a stalled API server or an
// interrupted run, and a cancelled parent must not mean no diagnostics at all.
func detachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), diagnosticsTimeout)
}

// listWorkloads gathers each kind separately, so that one request being refused
// drops only what it would have added. listedAny reports whether at least one
// list call succeeded, which is what separates "nothing exists" from "could not
// look".
func listWorkloads(
	ctx context.Context,
	cli kubernetes.Interface,
	ns, labelSelector string,
	palette console.Palette,
	logger *slog.Logger,
) ([]workloadFacts, bool) {
	options := metav1.ListOptions{LabelSelector: labelSelector}

	runtime, runtimeListed := listRuntimeWorkloads(ctx, cli, ns, options, palette, logger)
	owners, ownersListed := listOwnerWorkloads(ctx, cli, ns, options, palette, logger)

	return append(runtime, owners...), runtimeListed || ownersListed
}

func listRuntimeWorkloads(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	options metav1.ListOptions,
	palette console.Palette,
	logger *slog.Logger,
) ([]workloadFacts, bool) {
	var (
		facts  []workloadFacts
		listed bool
	)

	if pods, err := cli.CoreV1().Pods(ns).List(ctx, options); err != nil {
		logDiagnosticsError(logger, "pods", ns, err)
	} else {
		listed = true

		for i := range pods.Items {
			facts = append(facts, podFacts(&pods.Items[i], palette))
		}
	}

	if services, err := cli.CoreV1().Services(ns).List(ctx, options); err != nil {
		logDiagnosticsError(logger, "services", ns, err)
	} else {
		listed = true

		for i := range services.Items {
			facts = append(facts, serviceFacts(&services.Items[i], palette))
		}
	}

	return facts, listed
}

// listOwnerWorkloads covers the objects that own the pods, because the failures
// that produce no pod at all (admission, quota, a missing service account) are
// reported as events on the Job or the ReplicaSet.
func listOwnerWorkloads(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	options metav1.ListOptions,
	palette console.Palette,
	logger *slog.Logger,
) ([]workloadFacts, bool) {
	var (
		facts  []workloadFacts
		listed bool
	)

	if jobs, err := cli.BatchV1().Jobs(ns).List(ctx, options); err != nil {
		logDiagnosticsError(logger, "jobs", ns, err)
	} else {
		listed = true

		for i := range jobs.Items {
			facts = append(facts, jobFacts(&jobs.Items[i], palette))
		}
	}

	if deployments, err := cli.AppsV1().Deployments(ns).List(ctx, options); err != nil {
		logDiagnosticsError(logger, "deployments", ns, err)
	} else {
		listed = true

		for i := range deployments.Items {
			facts = append(facts, deploymentFacts(&deployments.Items[i], palette))
		}
	}

	if replicaSets, err := cli.AppsV1().ReplicaSets(ns).List(ctx, options); err != nil {
		logDiagnosticsError(logger, "replicasets", ns, err)
	} else {
		listed = true

		for i := range replicaSets.Items {
			facts = append(facts, replicaSetFacts(&replicaSets.Items[i], palette))
		}
	}

	return facts, listed
}

func logDiagnosticsError(logger *slog.Logger, kind, ns string, err error) {
	logger.Debug("failed to list resources for diagnostics", "kind", kind, "namespace", ns, "error", err)
}

func podFacts(pod *corev1.Pod, palette console.Palette) workloadFacts {
	facts := workloadFacts{
		uid:   pod.UID,
		kind:  "pod",
		name:  pod.Name,
		title: fmt.Sprintf("pod %s: %s", pod.Name, colorPhase(pod.Status.Phase, palette)),
	}

	for i := range pod.Status.ContainerStatuses {
		status := &pod.Status.ContainerStatuses[i]

		switch {
		case status.State.Waiting != nil:
			// Yellow, not red: waiting is by definition not terminal, and a
			// routine ContainerCreating must not carry the strongest signal.
			facts.details = append(facts.details, palette.Warn(
				fmt.Sprintf("container %s waiting: %s", status.Name, describeWaiting(status.State.Waiting))))
		case status.State.Terminated != nil:
			line := fmt.Sprintf("container %s terminated: %s",
				status.Name, describeTerminatedBriefly(status.State.Terminated))
			if status.State.Terminated.ExitCode != 0 {
				line = palette.Bad(line)
			} else {
				line = palette.Dim(line)
			}

			facts.details = append(facts.details, line)
		}

		// A container that is waiting to be restarted says nothing about why it
		// stopped, so the previous run's exit is the only thing that does.
		if status.State.Waiting != nil && status.LastTerminationState.Terminated != nil {
			facts.details = append(facts.details, palette.Warn(
				fmt.Sprintf("container %s previously terminated: %s",
					status.Name, describeTerminatedBriefly(status.LastTerminationState.Terminated))))
		}
	}

	return facts
}

// colorPhase colors a pod phase by what it means: terminal failure red,
// still-pending yellow, healthy green.
func colorPhase(phase corev1.PodPhase, palette console.Palette) string {
	switch phase {
	case corev1.PodFailed:
		return palette.Bad(string(phase))
	case corev1.PodPending, corev1.PodUnknown:
		return palette.Warn(string(phase))
	case corev1.PodRunning, corev1.PodSucceeded:
		return palette.Good(string(phase))
	default:
		return string(phase)
	}
}

func describeWaiting(waiting *corev1.ContainerStateWaiting) string {
	if message := strings.TrimSpace(waiting.Message); message != "" {
		return waiting.Reason + ": " + message
	}

	return waiting.Reason
}

func describeTerminatedBriefly(terminated *corev1.ContainerStateTerminated) string {
	parts := []string{fmt.Sprintf("exit code %d", terminated.ExitCode)}

	if terminated.Signal != 0 {
		parts = append(parts, fmt.Sprintf("signal %d", terminated.Signal))
	}

	if terminated.Reason != "" && terminated.Reason != genericTerminationReason {
		parts = append(parts, "reason: "+terminated.Reason)
	}

	if message := strings.TrimSpace(terminated.Message); message != "" {
		parts = append(parts, "message: "+message)
	}

	return strings.Join(parts, ", ")
}

// serviceFacts reports the address, or the absence of one: a LoadBalancer that
// never gets an ingress entry is the whole of the load balancer timeout class.
func serviceFacts(service *corev1.Service, palette console.Palette) workloadFacts {
	facts := workloadFacts{
		uid:   service.UID,
		kind:  "service",
		name:  service.Name,
		title: palette.Dim(fmt.Sprintf("service %s: %s", service.Name, service.Spec.Type)),
	}

	var addresses []string

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}

		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
		}
	}

	if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
		if len(addresses) == 0 {
			facts.details = append(facts.details, palette.Bad("no external address assigned"))
		} else {
			facts.details = append(facts.details, palette.Good("external address: "+strings.Join(addresses, ", ")))
		}
	}

	return facts
}

func jobFacts(job *batchv1.Job, palette console.Palette) workloadFacts {
	title := fmt.Sprintf("job %s: %d active, %d succeeded, %d failed",
		job.Name, job.Status.Active, job.Status.Succeeded, job.Status.Failed)

	switch {
	case job.Status.Failed > 0:
		title = palette.Bad(title)
	case job.Status.Succeeded > 0:
		title = palette.Good(title)
	default:
		title = palette.Dim(title)
	}

	return workloadFacts{
		uid:       job.UID,
		kind:      "job",
		name:      job.Name,
		title:     title,
		quiet:     job.Status.Succeeded == 0 && job.Status.Failed == 0,
		bareTitle: palette.Dim("job " + job.Name + ":"),
	}
}

// dropShadowedQuietFacts removes owner rows that add nothing over the pods
// below them and have no events of their own.
func dropShadowedQuietFacts(facts []workloadFacts, events map[types.UID][]string) []workloadFacts {
	kept := facts[:0]

	for _, fact := range facts {
		if fact.quiet && len(events[fact.uid]) == 0 && hasPodOfWorkload(facts, fact.name) {
			continue
		}

		// Kept only for its events: print it without the lagging counters.
		if fact.quiet && len(events[fact.uid]) > 0 && fact.bareTitle != "" {
			fact.title = fact.bareTitle
		}

		kept = append(kept, fact)
	}

	return kept
}

func hasPodOfWorkload(facts []workloadFacts, workloadName string) bool {
	for _, fact := range facts {
		if fact.kind == "pod" && strings.HasPrefix(fact.name, workloadName+"-") {
			return true
		}
	}

	return false
}

func deploymentFacts(deployment *appsv1.Deployment, palette console.Palette) workloadFacts {
	return workloadFacts{
		uid:  deployment.UID,
		kind: "deployment",
		name: deployment.Name,
		title: colorReadiness(fmt.Sprintf("deployment %s: %d/%d ready",
			deployment.Name, deployment.Status.ReadyReplicas, deployment.Status.Replicas),
			deployment.Status.ReadyReplicas, deployment.Status.Replicas, palette),
	}
}

func replicaSetFacts(replicaSet *appsv1.ReplicaSet, palette console.Palette) workloadFacts {
	return workloadFacts{
		uid:  replicaSet.UID,
		kind: "replicaset",
		name: replicaSet.Name,
		title: colorReadiness(fmt.Sprintf("replicaset %s: %d/%d ready",
			replicaSet.Name, replicaSet.Status.ReadyReplicas, replicaSet.Status.Replicas),
			replicaSet.Status.ReadyReplicas, replicaSet.Status.Replicas, palette),
	}
}

// colorReadiness colors an owner workload's readiness: short of desired yellow,
// fully ready green, empty neutral.
func colorReadiness(line string, ready, desired int32, palette console.Palette) string {
	switch {
	case desired > 0 && ready < desired:
		return palette.Warn(line)
	case desired > 0:
		return palette.Good(line)
	default:
		return palette.Dim(line)
	}
}

// warningEventsByUID makes one list per namespace and keys it by the UID of the
// object each event is about. Matching by name would attribute an event to the
// wrong object, since the chart's sshd Deployment and Service share a name.
func warningEventsByUID(
	ctx context.Context,
	cli kubernetes.Interface,
	ns string,
	logger *slog.Logger,
) map[types.UID][]string {
	list, err := cli.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: "type=Warning",
		// A shared namespace can hold a lot of events, and this runs on a
		// failure path that should not fetch more than it can ever print.
		Limit: maxEventsList,
	})
	if err != nil {
		logDiagnosticsError(logger, "events", ns, err)

		return nil
	}

	byUID := make(map[types.UID][]string)

	// Kept in list order, which is roughly oldest first: when an object has more
	// warnings than the cap, the earliest ones are the closest to the root cause.
	for i := range list.Items {
		event := &list.Items[i]

		uid := event.InvolvedObject.UID
		if event.Type != corev1.EventTypeWarning || uid == "" || len(byUID[uid]) >= maxEventsPerObject {
			continue
		}

		byUID[uid] = append(byUID[uid], formatEvent(event))
	}

	return byUID
}

// formatEvent uses the API's own repeat count rather than deduplicating by hand.
// A leading "Error: " in the message is the kubelet's boilerplate, already
// covered by the event being a warning, so it is trimmed rather than stuttered.
func formatEvent(event *corev1.Event) string {
	message := strings.TrimPrefix(strings.TrimSpace(event.Message), "Error: ")
	line := fmt.Sprintf("warning %s: %s", event.Reason, message)

	if event.Count > 1 {
		line += fmt.Sprintf(" (x%d)", event.Count)
	}

	return line
}
