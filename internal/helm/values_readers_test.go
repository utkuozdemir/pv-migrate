package helm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/strvals"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
)

// The narration reads the merged values, and a user's --helm-set turns a typed
// list into plain values on the way, so both shapes have to read the same.
func TestDescribeMountsReadsBothShapes(t *testing.T) {
	t.Parallel()

	typed := map[string]any{
		"pvcMounts": []map[string]any{
			{"name": "src", "mountPath": "/source", "readOnly": true},
			{"name": "dest", "mountPath": "/dest"},
		},
	}
	assert.Equal(t, "mounts src at /source read-only, mounts dest at /dest", helm.DescribeMounts(typed))

	parsed := map[string]any{}
	require.NoError(t, strvals.ParseInto("sshd.pvcMounts[0].name=src,sshd.pvcMounts[0].mountPath=/elsewhere", parsed))

	section, ok := parsed["sshd"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "mounts src at /elsewhere", helm.DescribeMounts(section),
		"a mount path the user set is the one that is told")

	assert.Empty(t, helm.DescribeMounts(map[string]any{}))
}

func TestComponentReaders(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"sshd":  map[string]any{"enabled": true, "namespace": "source-ns"},
		"rsync": map[string]any{"enabled": false, "namespace": "dest-ns"},
		"rclone": map[string]any{
			"enabled": true, "namespace": "ns", "networkPolicy": map[string]any{"enabled": false},
		},
	}

	sshd, ok := helm.EnabledComponent(values, "sshd")
	require.True(t, ok)
	assert.Equal(t, "source-ns", helm.ComponentNamespace(sshd))
	assert.True(t, helm.NetworkPolicyOn(sshd), "on unless switched off")

	_, ok = helm.EnabledComponent(values, "rsync")
	assert.False(t, ok, "a disabled component is not part of the release")

	_, ok = helm.EnabledComponent(values, "missing")
	assert.False(t, ok)

	rclone, ok := helm.EnabledComponent(values, "rclone")
	require.True(t, ok)
	assert.False(t, helm.NetworkPolicyOn(rclone))
}
