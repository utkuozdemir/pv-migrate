package helm_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/engine"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/rclone"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

// FuzzChartCommandYAML checks the end-to-end invariant that shell.CheckSingleLine
// exists for: every command string accepted by rsync.Cmd.Build or rclone.Cmd.Build
// must produce valid Kubernetes Job YAML when interpolated into the chart's block
// scalar, and the decoded container script must carry the exact command on its own
// line.
func FuzzChartCommandYAML(f *testing.F) {
	f.Add("/source/", "/dest/", "sshd.default", "remote:bucket/backup/")
	f.Add("/source/my dir/it's here/", "/dest/tab\there/", "10.0.0.1", "remote:bucket/with space/")
	f.Add("/source/#not-a-comment: x", "/dest/---", "[::1]", "remote:bucket/{a,b}/")
	f.Add("/source/\nbreak", "/dest/", "host", "remote:b/")
	f.Add("/source/\u0085nel", "/dest/\u2028ls", "host\ufffe", "remote:b/\xff")
	f.Add("/source/\U0001fffe", "/dest/\U0010ffff", "host", "remote:b/\ufeff")

	loaded, err := helm.LoadChart("")
	require.NoError(f, err)

	f.Fuzz(func(t *testing.T, srcPath, destPath, sshHost, remotePath string) {
		rsyncCmd, rsyncErr := (&rsync.Cmd{
			SrcUseSSH:  true,
			SrcSSHHost: sshHost,
			SrcPath:    srcPath,
			DestPath:   destPath,
		}).Build()
		if rsyncErr == nil {
			assertJobManifestParses(t, loaded, "rsync", "pv-migrate/templates/rsync/job.yaml", rsyncCmd)
		}

		rcloneCmd, rcloneErr := (&rclone.Cmd{
			Direction:  rclone.DirectionBackup,
			LocalPath:  srcPath,
			RemotePath: remotePath,
			ConfigPath: destPath,
		}).Build()
		if rcloneErr == nil {
			assertJobManifestParses(t, loaded, "rclone", "pv-migrate/templates/rclone/job.yaml", rcloneCmd)
		}
	})
}

func assertJobManifestParses(
	t *testing.T,
	loaded *chart.Chart,
	componentName, templatePath, wantCmd string,
) {
	t.Helper()

	values, err := commonutil.ToRenderValues(loaded, component(componentName, map[string]any{
		"command": wantCmd,
	}), releaseOptions(), nil)
	require.NoError(t, err)

	files, err := engine.Render(loaded, values)
	require.NoError(t, err)

	var job batchv1.Job

	require.NoError(t, yaml.Unmarshal([]byte(files[templatePath]), &job))
	require.NotEmpty(t, job.Spec.Template.Spec.Containers)

	command := job.Spec.Template.Spec.Containers[0].Command
	require.NotEmpty(t, command)

	lines := strings.Split(command[len(command)-1], "\n")
	require.True(t, slices.ContainsFunc(lines, func(line string) bool {
		return strings.TrimSpace(line) == wantCmd
	}), "decoded script does not carry the exact command line %q", wantCmd)
}
