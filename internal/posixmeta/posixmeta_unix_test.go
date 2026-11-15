//go:build !windows

package posixmeta_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/posixmeta"
)

func TestSave_SkipsNamedPipe(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	fifoPath := filepath.Join(srcDir, "pipe")
	require.NoError(t, syscall.Mkfifo(fifoPath, 0o600))
	writeTestFile(t, filepath.Join(srcDir, "regular.txt"), []byte("ok"), 0o600)

	manifestPath := filepath.Join(t.TempDir(), "posix.gz")
	require.NoError(t, posixmeta.Save(srcDir, manifestPath, posixmeta.SaveOptions{}))

	dstFile := filepath.Join(dstDir, "regular.txt")
	writeTestFile(t, dstFile, []byte("ok"), 0o644)

	require.NoError(t, posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: dstDir,
		NoChown:   true,
	}))

	info, err := os.Lstat(dstFile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())

	_, fifoErr := os.Lstat(filepath.Join(dstDir, "pipe"))
	require.ErrorIs(t, fifoErr, fs.ErrNotExist)
}
