package bucketstorage

import (
	"context"
	"testing"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

// The real values builder is handed to the permission check, so that the two
// stay in agreement about the shape of the values. See the strategy package's
// counterpart for why.
func TestRcloneValuesFollowNetworkPolicyPermission(t *testing.T) {
	t.Parallel()

	info := &pvc.Info{
		Claim: &corev1.PersistentVolumeClaim{Namespace: "ns", Name: "pvc"},
	}

	values := buildHelmValues("ns", &Request{}, info, "[remote]\ntype = s3\n", "rclone sync '/data' 'remote:b/'",
		true, "", "")

	denyAll := func(_ context.Context, _ string) (bool, error) { return false, nil }

	helm.DisableNetworkPoliciesWhereForbidden(t.Context(), values, denyAll, slogt.New(t))

	rclone, ok := values["rclone"].(map[string]any)
	require.True(t, ok)

	policy, ok := rclone["networkPolicy"].(map[string]any)
	require.True(t, ok, "the rclone component should have had its policy switched off")
	assert.Equal(t, false, policy["enabled"])

	// A policy the user asks for is still attempted.
	merged, err := mergeHelmValues(values, &Request{HelmValues: []string{"rclone.networkPolicy.enabled=true"}},
		slogt.New(t))
	require.NoError(t, err)

	mergedRclone, ok := merged["rclone"].(map[string]any)
	require.True(t, ok)

	mergedPolicy, ok := mergedRclone["networkPolicy"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, mergedPolicy["enabled"])
}
