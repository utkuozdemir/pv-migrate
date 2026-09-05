package strategy

import (
	"context"
	"testing"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

// These tests hand the real value builders to the permission check, so that the
// two stay in agreement about the shape of the values: the component's enabled
// flag, its namespace and where its policy switch lives. A builder that drifts
// makes the check skip the component, and the first to notice would be a
// restricted account whose install fails over a policy it cannot create.

func pvcInfoForTest(namespace, name string) *pvc.Info {
	return &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{
			Namespace: namespace, Name: name,
		},
	}
}

func denyAll(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func policyEnabled(t *testing.T, values map[string]any, component string) any {
	t.Helper()

	section, ok := values[component].(map[string]any)
	require.True(t, ok, component)

	policy, ok := section["networkPolicy"].(map[string]any)
	require.True(t, ok, "%s has no networkPolicy section", component)

	return policy["enabled"]
}

func TestTwoReleaseValuesFollowNetworkPolicyPermission(t *testing.T) {
	t.Parallel()

	side := componentSide{info: pvcInfoForTest("ns", "pvc"), mountPath: srcMountPath, readOnly: true}
	values := map[string]any{
		sshdComponent:  buildSshdHelmValues(side, "ssh-ed25519 AAAA"),
		rsyncComponent: buildRsyncHelmValues(side, "rsync -a '/source/' '/dest/'", "key", "/tmp/id_ed25519"),
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, denyAll, slogt.New(t))

	assert.Equal(t, false, policyEnabled(t, values, sshdComponent))
	assert.Equal(t, false, policyEnabled(t, values, rsyncComponent))

	// A policy the user asks for is still attempted: the user's values are merged
	// on top of the checked ones.
	req := &migration.Request{HelmValues: []string{"sshd.networkPolicy.enabled=true"}}

	merged, err := getMergedHelmValues(values, req, slogt.New(t))
	require.NoError(t, err)

	assert.Equal(t, true, policyEnabled(t, merged, sshdComponent))
	assert.Equal(t, false, policyEnabled(t, merged, rsyncComponent))
}

func TestMountValuesNeedNoNetworkPolicyCheck(t *testing.T) {
	t.Parallel()

	mig := &migration.Migration{
		Request:    &migration.Request{},
		SourceInfo: pvcInfoForTest("ns", "src"),
		DestInfo:   pvcInfoForTest("ns", "dest"),
	}

	values := buildMountHelmValues(mig, "rsync -a '/source/' '/dest/'")

	neverAsked := func(_ context.Context, namespace string) (bool, error) {
		t.Fatalf("the mount pod uses no network, its values should not be checked (asked about %q)", namespace)

		return false, nil
	}

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, neverAsked, slogt.New(t))

	assert.Equal(t, false, policyEnabled(t, values, rsyncComponent))
}
