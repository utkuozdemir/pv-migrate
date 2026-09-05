package helm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
)

// verdicts answers the permission check from a table, one entry per namespace,
// and records what was asked. A namespace without an entry fails the check, as
// an unreachable authorizer would.
func verdicts(allowed map[string]bool, asked *[]string) helm.CanCreateNetworkPoliciesFunc {
	return func(_ context.Context, namespace string) (bool, error) {
		*asked = append(*asked, namespace)

		verdict, known := allowed[namespace]
		if !known {
			return false, errors.New("authorizer unavailable")
		}

		return verdict, nil
	}
}

func componentValues(enabled bool, namespace string) map[string]any {
	return map[string]any{
		"enabled":   enabled,
		"namespace": namespace,
		"pvcMounts": []map[string]any{{"name": "pvc", "mountPath": "/data"}},
	}
}

// policyEnabled returns the component's networkPolicy.enabled value and whether
// it is set at all. The chart default applies when it is not set.
func policyEnabled(t *testing.T, values map[string]any, component string) (bool, bool) {
	t.Helper()

	section, ok := values[component].(map[string]any)
	require.True(t, ok)

	policy, ok := section["networkPolicy"].(map[string]any)
	if !ok {
		return false, false
	}

	enabled, ok := policy["enabled"].(bool)

	return enabled, ok
}

func TestDisableNetworkPolicies_Allowed(t *testing.T) {
	t.Parallel()

	var asked []string

	values := map[string]any{
		"sshd":  componentValues(true, "ns"),
		"rsync": componentValues(true, "ns"),
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, verdicts(map[string]bool{"ns": true}, &asked),
		slogt.New(t))

	_, set := policyEnabled(t, values, "sshd")
	assert.False(t, set, "an allowed namespace should keep the chart default")

	_, set = policyEnabled(t, values, "rsync")
	assert.False(t, set)

	assert.Equal(t, []string{"ns"}, asked, "one namespace is checked once")
}

func TestDisableNetworkPolicies_Forbidden(t *testing.T) {
	t.Parallel()

	var asked []string

	values := map[string]any{
		"sshd":  componentValues(true, "source"),
		"rsync": componentValues(true, "dest"),
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values,
		verdicts(map[string]bool{"source": true, "dest": false}, &asked), slogt.New(t))

	_, set := policyEnabled(t, values, "sshd")
	assert.False(t, set, "the allowed side keeps its policy")

	enabled, set := policyEnabled(t, values, "rsync")
	assert.True(t, set)
	assert.False(t, enabled, "the forbidden side loses its policy")

	assert.ElementsMatch(t, []string{"source", "dest"}, asked)
}

func TestDisableNetworkPolicies_DisabledComponent(t *testing.T) {
	t.Parallel()

	var asked []string

	values := map[string]any{
		"sshd":  componentValues(false, "other"),
		"rsync": componentValues(true, "ns"),
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, verdicts(map[string]bool{"ns": false}, &asked),
		slogt.New(t))

	assert.Equal(t, []string{"ns"}, asked, "only the enabled component's namespace is asked about")

	_, set := policyEnabled(t, values, "sshd")
	assert.False(t, set)
}

func TestDisableNetworkPolicies_PolicyAlreadyOff(t *testing.T) {
	t.Parallel()

	var asked []string

	section := componentValues(true, "ns")
	section["networkPolicy"] = map[string]any{"enabled": false}
	values := map[string]any{"rsync": section}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, verdicts(map[string]bool{}, &asked), slogt.New(t))

	assert.Empty(t, asked, "a policy that is off already has nothing to check")
}

func TestDisableNetworkPolicies_FailedCheck(t *testing.T) {
	t.Parallel()

	var asked []string

	values := map[string]any{
		"sshd":  componentValues(true, "ns"),
		"rsync": componentValues(true, "ns"),
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, verdicts(map[string]bool{}, &asked), slogt.New(t))

	for _, component := range []string{"sshd", "rsync"} {
		enabled, set := policyEnabled(t, values, component)
		assert.True(t, set, component)
		assert.False(t, enabled,
			"an unanswered check drops the policy, so an account that could migrate before still can")
	}

	assert.Equal(t, []string{"ns"}, asked, "a failed check is not repeated for the same namespace")
}

func TestDisableNetworkPolicies_ExistingSection(t *testing.T) {
	t.Parallel()

	var asked []string

	section := componentValues(true, "ns")
	section["networkPolicy"] = map[string]any{"enabled": true, "extra": "kept"}
	values := map[string]any{"rsync": section}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, verdicts(map[string]bool{"ns": false}, &asked),
		slogt.New(t))

	policy, ok := section["networkPolicy"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, policy["enabled"])
	assert.Equal(t, "kept", policy["extra"])
}
