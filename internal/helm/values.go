package helm

import (
	"context"
	"log/slog"
	"maps"
	"slices"
)

const (
	keyEnabled       = "enabled"
	keyNamespace     = "namespace"
	keyNetworkPolicy = "networkPolicy"
)

// CanCreateNetworkPoliciesFunc reports whether NetworkPolicy objects may be
// created in the namespace.
type CanCreateNetworkPoliciesFunc func(ctx context.Context, namespace string) (bool, error)

// DisableNetworkPoliciesWhereForbidden turns the network policy off for every
// enabled chart component whose namespace the current account cannot create
// NetworkPolicy objects in.
//
// The chart creates an allow-all policy for each of its own pods by default, so
// that a transfer works in a namespace isolated with network policies without
// the user having to know about them. For an account without that permission the
// install would fail over an object that is a no-op wherever no other policy
// exists, so the permission is checked first and the policy is dropped instead of
// the install. A check that cannot be made drops the policy too: an account that
// could run a transfer before this default must still be able to. A value the
// user sets explicitly still wins, because user values are merged on top of
// these afterwards.
//
// The values are the chart's top-level sections, one per component, and the
// check runs once per namespace. A component whose policy is already off is not
// checked.
func DisableNetworkPoliciesWhereForbidden(
	ctx context.Context,
	values map[string]any,
	canCreate CanCreateNetworkPoliciesFunc,
	logger *slog.Logger,
) {
	allowedByNamespace := make(map[string]bool)

	for _, component := range slices.Sorted(maps.Keys(values)) {
		section, ok := values[component].(map[string]any)
		if !ok {
			continue
		}

		namespace := namespaceToCheck(section)
		if namespace == "" {
			continue
		}

		allowed, checked := allowedByNamespace[namespace]
		if !checked {
			allowed = networkPoliciesAllowed(ctx, canCreate, namespace, logger)
			allowedByNamespace[namespace] = allowed
		}

		if allowed {
			continue
		}

		policy, ok := section[keyNetworkPolicy].(map[string]any)
		if !ok {
			policy = make(map[string]any)
			section[keyNetworkPolicy] = policy
		}

		policy[keyEnabled] = false
	}
}

// namespaceToCheck returns the namespace whose permission decides the
// component's policy, or an empty string when there is nothing to decide: the
// component is not part of the release, or its policy is off already.
func namespaceToCheck(section map[string]any) string {
	if section[keyEnabled] != true {
		return ""
	}

	if policy, ok := section[keyNetworkPolicy].(map[string]any); ok && policy[keyEnabled] == false {
		return ""
	}

	namespace, _ := section[keyNamespace].(string)

	return namespace
}

// networkPoliciesAllowed asks once for one namespace and logs the outcome.
func networkPoliciesAllowed(
	ctx context.Context, canCreate CanCreateNetworkPoliciesFunc, namespace string, logger *slog.Logger,
) bool {
	allowed, err := canCreate(ctx, namespace)
	if err != nil {
		logger.Warn("🔶 Could not check whether network policies can be created, continuing without them. "+
			"A transfer that uses the network will not connect in a namespace with default-deny policies",
			"namespace", namespace, "error", err)

		return false
	}

	if !allowed {
		logger.Warn("🔶 Not allowed to create network policies in the namespace, continuing without them. "+
			"A transfer that uses the network will not connect in a namespace with default-deny policies",
			"namespace", namespace)
	}

	return allowed
}
