package helm_test

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rsync"
)

// The Job scripts decide what the container exits with, and they used to collapse
// every failure into 1. The control flow is what these tests are about, so they
// run the rendered script under a real shell with a stand-in data mover. Retries
// are zeroed, since the chart defaults would otherwise walk eleven attempts with
// a sleep between each.

func rsyncScript(t *testing.T, command string, maxRetries int) string {
	t.Helper()

	return containerScript(t, render(t, map[string]any{
		"rsync": map[string]any{
			"enabled":            true,
			"namespace":          "default",
			"command":            command,
			"maxRetries":         maxRetries,
			"retryPeriodSeconds": 0,
			"pvcMounts":          []any{map[string]any{"name": "pvc", "mountPath": "/source"}},
		},
	}), "rsync")
}

func rcloneScript(t *testing.T, values map[string]any) string {
	t.Helper()

	rclone := map[string]any{
		"enabled":            true,
		"namespace":          "default",
		"maxRetries":         0,
		"retryPeriodSeconds": 0,
		"pvcMounts":          []any{map[string]any{"name": "pvc", "mountPath": "/data"}},
	}

	maps.Copy(rclone, values)

	return containerScript(t, render(t, map[string]any{"rclone": rclone}), "rclone")
}

// runScript runs the script the way the container does and returns its exit code
// along with everything it printed.
func runScript(t *testing.T, script string, env ...string) (int, string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)

	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.CombinedOutput()
	t.Log(string(out))

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}

	require.NoError(t, err)

	return 0, string(out)
}

// exitingMover is a stand-in data mover that exits with the given code.
func exitingMover(code int) string {
	return fmt.Sprintf("sh -c 'exit %d'", code)
}

func TestRsyncScriptPreservesTheExitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		moverCode int
		wantCode  int
	}{
		"success":     {moverCode: 0, wantCode: 0},
		"usage error": {moverCode: 1, wantCode: 1},
		"partial":     {moverCode: 23, wantCode: 23},
		"vanished":    {moverCode: 24, wantCode: 0},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, _ := runScript(t, rsyncScript(t, exitingMover(tt.moverCode), 0))
			assert.Equal(t, tt.wantCode, code)
		})
	}
}

// TestRsyncScriptMarksVanishedFiles pins the marker the client scans for. Without
// it, a run that skipped files would report a plain success.
func TestRsyncScriptMarksVanishedFiles(t *testing.T) {
	t.Parallel()

	code, out := runScript(t, rsyncScript(t, exitingMover(24), 0))

	assert.Equal(t, 0, code)
	// The client constant, not a copied literal: this assertion is what keeps
	// the template's wording and the client's scan from drifting apart.
	assert.Contains(t, out, rsync.VanishedFilesMarker)
}

// TestRsyncScriptRetriesUntilItSucceeds pins the loop control flow that the exit
// code capture rewrote.
func TestRsyncScriptRetriesUntilItSucceeds(t *testing.T) {
	t.Parallel()

	counter := filepath.Join(t.TempDir(), "attempts")
	mover := fmt.Sprintf("sh -c 'echo x >> %s; if [ $(wc -l < %s) -ge 2 ]; then exit 0; else exit 12; fi'",
		counter, counter)

	code, _ := runScript(t, rsyncScript(t, mover, 1))
	assert.Equal(t, 0, code)
	assert.Equal(t, 2, countLines(t, counter))
}

// TestRsyncScriptDoesNotRetryUsageErrors: a typo in the extra arguments is
// deterministic, so retrying it only spends the budget before the user hears
// anything.
func TestRsyncScriptDoesNotRetryUsageErrors(t *testing.T) {
	t.Parallel()

	counter := filepath.Join(t.TempDir(), "attempts")
	mover := fmt.Sprintf("sh -c 'echo x >> %s; exit 1'", counter)

	code, _ := runScript(t, rsyncScript(t, mover, 3))
	assert.Equal(t, 1, code)
	assert.Equal(t, 1, countLines(t, counter))
}

func TestRcloneScriptPreservesTheExitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		moverCode int
		wantCode  int
	}{
		"success":     {moverCode: 0, wantCode: 0},
		"usage error": {moverCode: 1, wantCode: 1},
		"directory":   {moverCode: 3, wantCode: 3},
		// 24 is an rsync code and means nothing here, so it is passed through.
		"twenty four": {moverCode: 24, wantCode: 24},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, _ := runScript(t, rcloneScript(t, map[string]any{"command": exitingMover(tt.moverCode)}))
			assert.Equal(t, tt.wantCode, code)
		})
	}
}

// TestRcloneScriptFailsWhenTheMetadataUploadFails: the metadata object is how a
// restore finds a backup, so a backup without one is not a backup and the job may
// not report success. The container exits with the upload's real code, not a
// collapsed sentinel.
func TestRcloneScriptFailsWhenTheMetadataUploadFails(t *testing.T) {
	t.Parallel()

	script := rcloneScript(t, map[string]any{
		"command":            exitingMover(0),
		"metadataBase64":     "dGVzdAo=",
		"metadataRemotePath": "remote:bucket/backup.meta.yaml",
	})

	code, out := runScript(t, script, "PATH="+failingRcloneDir(t, 7)+string(os.PathListSeparator)+os.Getenv("PATH"))

	assert.Equal(t, 7, code, "the metadata upload's own exit code has to survive")
	assert.Contains(t, out, "metadata upload failed with exit code 7")
}

// TestRcloneScriptRetriesUsageErrors pins that rclone, unlike rsync, retries an
// exit 1: rclone was observed exiting 1 for a transient credentials failure, so
// treating it as a deterministic usage error would drop the retry budget where
// it is needed.
func TestRcloneScriptRetriesUsageErrors(t *testing.T) {
	t.Parallel()

	counter := filepath.Join(t.TempDir(), "attempts")
	mover := fmt.Sprintf("sh -c 'echo x >> %s; exit 1'", counter)

	code, _ := runScript(t, rcloneScript(t, map[string]any{"command": mover, "maxRetries": 1}))
	assert.Equal(t, 1, code)
	assert.Equal(t, 2, countLines(t, counter))
}

// failingRcloneDir returns a directory holding an rclone that always fails with
// the given code, to be put in front of PATH.
func failingRcloneDir(t *testing.T, code int) string {
	t.Helper()

	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", code)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rclone"), []byte(script), 0o755)) //nolint:gosec

	return dir
}

func countLines(t *testing.T, path string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	count := 0

	for _, b := range data {
		if b == '\n' {
			count++
		}
	}

	return count
}
