package helm_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"
	"sigs.k8s.io/yaml"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
)

// The Job templates put a command string inside a YAML block scalar and run it
// with `sh -c`, which makes every value that reaches that script two things at
// once: YAML, and shell. These tests cover the second consumer, since the Go code
// that builds those values can only be trusted about the first.

// TestRenderedMetadataPathIsNotShellInput is the reason the metadata upload reads
// its remote path from the environment instead of having it templated in.
//
// The path is assembled from --bucket, --prefix and --name. Rendering it inline
// meant rendering it inside shell double quotes, where a command substitution
// still runs, so those flags were shell input for the rclone job. The flags are
// validated now as well, but the template must not be the thing that depends on
// that.
func TestRenderedMetadataPathIsNotShellInput(t *testing.T) {
	t.Parallel()

	const substitution = `remote:$(touch /tmp/pwned)/backup.meta.yaml`

	rendered := render(t, map[string]any{
		"rclone": map[string]any{
			"enabled":            true,
			"namespace":          "default",
			"command":            "rclone sync '/data' 'remote:bucket/name/'",
			"metadataBase64":     "dGVzdAo=",
			"metadataRemotePath": substitution,
			"pvcMounts":          []any{map[string]any{"name": "pvc", "mountPath": "/data"}},
		},
	})

	script := containerScript(t, rendered, "rclone")

	assert.NotContains(t, script, substitution,
		"the metadata path is rendered into the script, where a command substitution in it would run")
	assert.Contains(t, script, `"$PV_MIGRATE_METADATA_REMOTE_PATH"`,
		"the script should read the metadata path from the environment")
}

// TestRenderedCommandWithLineBreakFails records why the command builders refuse a
// line break rather than quoting it. Quoting one is valid shell, but the command
// is interpolated into a block scalar, so the chart stops rendering and the error
// names a template line rather than the flag that caused it. The builders reject
// the value first; this test pins the reason.
func TestRenderedCommandWithLineBreakFails(t *testing.T) {
	t.Parallel()

	loaded, err := helm.LoadChart("")
	require.NoError(t, err)

	values, err := commonutil.ToRenderValues(loaded, map[string]any{
		"rsync": map[string]any{
			"enabled":   true,
			"namespace": "default",
			"command":   "rsync -av '/source/a\nb/' '/dest/'",
			"pvcMounts": []any{map[string]any{"name": "pvc", "mountPath": "/source"}},
		},
	}, releaseOptions(), nil)
	require.NoError(t, err)

	files, err := engine.Render(loaded, values)
	require.NoError(t, err, "rendering the template itself does not fail, the YAML it produces is invalid")

	var job map[string]any

	err = yaml.Unmarshal([]byte(files["pv-migrate/templates/rsync/job.yaml"]), &job)
	require.Error(t, err,
		"a line break in the command breaks the rendered YAML, which is why it is rejected earlier")
}

func render(t *testing.T, values map[string]any) map[string]string {
	t.Helper()

	loaded, err := helm.LoadChart("")
	require.NoError(t, err)

	renderValues, err := commonutil.ToRenderValues(loaded, values, releaseOptions(), nil)
	require.NoError(t, err)

	files, err := engine.Render(loaded, renderValues)
	require.NoError(t, err)

	return files
}

func releaseOptions() common.ReleaseOptions {
	return common.ReleaseOptions{Name: "pv-migrate-test", Namespace: "default"}
}

// containerScript returns the `sh -c` script of the named container in whichever
// rendered manifest defines it.
func containerScript(t *testing.T, files map[string]string, container string) string {
	t.Helper()

	for _, content := range files {
		if strings.TrimSpace(content) == "" {
			continue
		}

		var manifest struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name    string   `json:"name"`
							Command []string `json:"command"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}

		if err := yaml.Unmarshal([]byte(content), &manifest); err != nil {
			continue
		}

		for _, c := range manifest.Spec.Template.Spec.Containers {
			if c.Name == container && len(c.Command) > 0 {
				return c.Command[len(c.Command)-1]
			}
		}
	}

	t.Fatalf("no container named %q found in the rendered chart", container)

	return ""
}

// TestNetworkPolicyFollowsItsComponent pins the guard on the network policy
// templates. The policies are on by default, and a release carries only some of
// the components, so a policy must follow its component rather than its own
// switch alone. Without the guard a migration release rendered an rclone policy,
// and a release for one component rendered the policies of the others, without
// a namespace of their own.
func TestNetworkPolicyFollowsItsComponent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		values       map[string]any
		wantPolicies []string
	}{
		{
			name:         "rsync only",
			values:       component("rsync", map[string]any{"command": "rsync -a '/source/' '/dest/'"}),
			wantPolicies: []string{"rsync"},
		},
		{
			name:         "sshd only",
			values:       component("sshd", map[string]any{"publicKey": "ssh-ed25519 AAAA"}),
			wantPolicies: []string{"sshd"},
		},
		{
			name:         "rclone only",
			values:       component("rclone", map[string]any{"command": "rclone sync '/data' 'remote:b/'"}),
			wantPolicies: []string{"rclone"},
		},
		{
			name: "rsync with its policy switched off",
			values: component("rsync", map[string]any{
				"command":       "rsync -a '/source/' '/dest/'",
				"networkPolicy": map[string]any{"enabled": false},
			}),
			wantPolicies: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rendered := render(t, tc.values)

			for _, name := range []string{"sshd", "rsync", "rclone"} {
				policy := strings.TrimSpace(rendered["pv-migrate/templates/"+name+"/networkpolicy.yaml"])

				if slices.Contains(tc.wantPolicies, name) {
					assert.Contains(t, policy, "kind: NetworkPolicy", "%s should get its policy", name)
				} else {
					assert.Empty(t, policy, "%s should get no policy", name)
				}
			}
		})
	}
}

// component returns values with one enabled component in the release namespace.
func component(name string, extra map[string]any) map[string]any {
	section := map[string]any{
		"enabled":   true,
		"namespace": "default",
		"pvcMounts": []any{map[string]any{"name": "pvc", "mountPath": "/data"}},
	}

	maps.Copy(section, extra)

	return map[string]any{name: section}
}
