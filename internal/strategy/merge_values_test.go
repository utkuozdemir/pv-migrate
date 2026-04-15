package strategy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

func TestGetMergedHelmValues_BaseOnly(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": "default",
			"publicKey": "ssh-ed25519 AAAA...",
			"pvcMounts": []map[string]any{
				{"name": "my-pvc", "mountPath": "/source", "readOnly": true},
			},
		},
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           "default",
			"privateKeyMount":     true,
			"privateKey":          "-----BEGIN OPENSSH PRIVATE KEY-----",
			"privateKeyMountPath": "/tmp/id_ed25519",
			"command":             "rsync -avzs /source/ /dest/",
			"pvcMounts": []map[string]any{
				{"name": "my-pvc", "mountPath": "/dest"},
			},
		},
	}

	req := &migration.Request{}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, true, gotSSHD["enabled"])
	assert.Equal(t, "default", gotSSHD["namespace"])
	assert.Equal(t, "ssh-ed25519 AAAA...", gotSSHD["publicKey"])

	gotRsync, ok := got["rsync"].(map[string]any)
	require.True(t, ok, "rsync key should be a map[string]any")

	assert.Equal(t, true, gotRsync["enabled"])
	assert.Equal(t, "rsync -avzs /source/ /dest/", gotRsync["command"])
	assert.Equal(t, true, gotRsync["privateKeyMount"])
}

func TestGetMergedHelmValues_ImageTagInjected(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd":  map[string]any{"enabled": true},
		"rsync": map[string]any{"enabled": true},
	}

	req := &migration.Request{ImageTag: "v2.0.0"}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	sshdImage, ok := gotSSHD["image"].(map[string]any)
	require.True(t, ok, "sshd.image should be a map[string]any")

	assert.Equal(t, "v2.0.0", sshdImage["tag"])

	gotRsync, ok := got["rsync"].(map[string]any)
	require.True(t, ok, "rsync key should be a map[string]any")

	rsyncImage, ok := gotRsync["image"].(map[string]any)
	require.True(t, ok, "rsync.image should be a map[string]any")

	assert.Equal(t, "v2.0.0", rsyncImage["tag"])
}

func TestGetMergedHelmValues_HelmSetOverridesBase(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": "original-ns",
		},
	}

	req := &migration.Request{
		HelmValues: []string{"sshd.namespace=overridden-ns"},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, "overridden-ns", gotSSHD["namespace"])
	assert.Equal(t, true, gotSSHD["enabled"])
}

func TestGetMergedHelmValues_HelmSetOverridesImageTag(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd":  map[string]any{"enabled": true},
		"rsync": map[string]any{"enabled": true},
	}

	req := &migration.Request{
		ImageTag:   "v2.0.0",
		HelmValues: []string{"sshd.image.tag=custom-tag"},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	sshdImage, ok := gotSSHD["image"].(map[string]any)
	require.True(t, ok, "sshd.image should be a map[string]any")

	// --helm-set should override the ImageTag injection
	assert.Equal(t, "custom-tag", sshdImage["tag"])

	gotRsync, ok := got["rsync"].(map[string]any)
	require.True(t, ok, "rsync key should be a map[string]any")

	rsyncImage, ok := gotRsync["image"].(map[string]any)
	require.True(t, ok, "rsync.image should be a map[string]any")

	// rsync should still get the ImageTag value
	assert.Equal(t, "v2.0.0", rsyncImage["tag"])
}

func TestGetMergedHelmValues_ValuesFileOverridesBase(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": "original",
			"publicKey": "ssh-ed25519 AAAA...",
		},
		"rsync": map[string]any{
			"enabled": true,
		},
	}

	valuesFile := filepath.Join(t.TempDir(), "override.yaml")
	err := os.WriteFile(valuesFile, []byte(`
sshd:
  namespace: from-file
  service:
    type: LoadBalancer
`), 0o600)
	require.NoError(t, err)

	req := &migration.Request{
		HelmValuesFiles: []string{valuesFile},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, "from-file", gotSSHD["namespace"])
	assert.Equal(t, true, gotSSHD["enabled"])
	assert.Equal(t, "ssh-ed25519 AAAA...", gotSSHD["publicKey"])

	service, ok := gotSSHD["service"].(map[string]any)
	require.True(t, ok, "sshd.service should be a map[string]any")

	assert.Equal(t, "LoadBalancer", service["type"])

	gotRsync, ok := got["rsync"].(map[string]any)
	require.True(t, ok, "rsync key should be a map[string]any")

	// rsync should be untouched
	assert.Equal(t, true, gotRsync["enabled"])
}

func TestGetMergedHelmValues_FullPriorityOrder(t *testing.T) {
	t.Parallel()

	// base values (lowest priority)
	base := map[string]any{
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": "from-base",
			"publicKey": "base-key",
		},
	}

	// values file (overrides base)
	valuesFile := filepath.Join(t.TempDir(), "vals.yaml")
	err := os.WriteFile(valuesFile, []byte(`
sshd:
  namespace: from-file
  publicKey: file-key
`), 0o600)
	require.NoError(t, err)

	// --helm-set (overrides values file)
	req := &migration.Request{
		HelmValuesFiles: []string{valuesFile},
		HelmValues:      []string{"sshd.publicKey=set-key"},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	// base value preserved when not overridden
	assert.Equal(t, true, gotSSHD["enabled"])
	// file overrides base
	assert.Equal(t, "from-file", gotSSHD["namespace"])
	// --set overrides file
	assert.Equal(t, "set-key", gotSSHD["publicKey"])
}

func realisticClusterIPBase() map[string]any {
	return map[string]any{
		"rsync": map[string]any{
			"enabled":             true,
			"namespace":           "dest-ns",
			"privateKeyMount":     true,
			"privateKey":          "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----",
			"privateKeyMountPath": "/tmp/id_ed25519",
			"pvcMounts":           []map[string]any{{"name": "dest-pvc", "mountPath": "/dest"}},
			"command":             "rsync -avzs -e 'ssh -o StrictHostKeyChecking=no' /source/ sshd-host:/dest/",
			"affinity":            map[string]any{},
		},
		"sshd": map[string]any{
			"enabled":   true,
			"namespace": "source-ns",
			"publicKey": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...",
			"pvcMounts": []map[string]any{{"name": "source-pvc", "mountPath": "/source", "readOnly": true}},
			"affinity":  map[string]any{},
		},
	}
}

func TestGetMergedHelmValues_RealisticRsyncValues(t *testing.T) {
	t.Parallel()

	req := &migration.Request{ImageTag: "v2.2.1"}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(realisticClusterIPBase(), req, logger)
	require.NoError(t, err)

	gotRsync, ok := got["rsync"].(map[string]any)
	require.True(t, ok, "rsync key should be a map[string]any")

	assert.Equal(t, true, gotRsync["enabled"])
	assert.Equal(t, "dest-ns", gotRsync["namespace"])
	assert.Equal(t, true, gotRsync["privateKeyMount"])
	assert.Equal(t, "/tmp/id_ed25519", gotRsync["privateKeyMountPath"])

	rsyncImage, ok := gotRsync["image"].(map[string]any)
	require.True(t, ok, "rsync.image should be a map[string]any")

	assert.Equal(t, "v2.2.1", rsyncImage["tag"])
}

func TestGetMergedHelmValues_RealisticSSHDValues(t *testing.T) {
	t.Parallel()

	req := &migration.Request{ImageTag: "v2.2.1"}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(realisticClusterIPBase(), req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, true, gotSSHD["enabled"])
	assert.Equal(t, "source-ns", gotSSHD["namespace"])
	assert.Equal(t, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...", gotSSHD["publicKey"])

	sshdImage, ok := gotSSHD["image"].(map[string]any)
	require.True(t, ok, "sshd.image should be a map[string]any")

	assert.Equal(t, "v2.2.1", sshdImage["tag"])

	// Verify pvcMounts are preserved
	sshdMounts, ok := gotSSHD["pvcMounts"].([]map[string]any)
	require.True(t, ok, "sshd.pvcMounts should be a []map[string]any")
	require.Len(t, sshdMounts, 1)

	assert.Equal(t, "source-pvc", sshdMounts[0]["name"])
	assert.Equal(t, "/source", sshdMounts[0]["mountPath"])
	assert.Equal(t, true, sshdMounts[0]["readOnly"])
}

func TestGetMergedHelmValues_EmptyBase(t *testing.T) {
	t.Parallel()

	req := &migration.Request{
		HelmValues: []string{"sshd.enabled=true"},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(map[string]any{}, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, true, gotSSHD["enabled"])
}

func TestGetMergedHelmValues_StringValuesOverride(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"sshd": map[string]any{
			"enabled": true,
		},
	}

	req := &migration.Request{
		// --set-string always produces string values
		HelmStringValues: []string{"sshd.namespace=string-ns"},
	}
	logger := slogt.New(t)

	got, err := getMergedHelmValues(base, req, logger)
	require.NoError(t, err)

	gotSSHD, ok := got["sshd"].(map[string]any)
	require.True(t, ok, "sshd key should be a map[string]any")

	assert.Equal(t, true, gotSSHD["enabled"])
	assert.Equal(t, "string-ns", gotSSHD["namespace"])
}
