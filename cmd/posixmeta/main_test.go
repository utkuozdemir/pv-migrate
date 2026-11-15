package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_SaveAndApply(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "posix.gz")

	require.NoError(t, os.Mkdir(filepath.Join(srcDir, "empty-dir"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "secret.txt"), []byte("secret"), 0o600))

	require.NoError(t, run([]string{"save", srcDir, manifestPath}))

	dstFile := filepath.Join(dstDir, "secret.txt")
	require.NoError(t, os.WriteFile(dstFile, []byte("secret"), 0o600))
	require.NoError(t, os.Chmod(dstFile, 0o644))

	require.NoError(t, run([]string{"apply", "--no-chown", "--mount-root", dstDir, dstDir, manifestPath}))

	fileInfo, err := os.Lstat(dstFile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), fileInfo.Mode().Perm())

	dirInfo, err := os.Lstat(filepath.Join(dstDir, "empty-dir"))
	require.NoError(t, err)
	assert.True(t, dirInfo.IsDir())
	assert.Equal(t, fs.FileMode(0o700), dirInfo.Mode().Perm())
}

func TestRun_ErrorsOnInvalidInvocation(t *testing.T) {
	t.Parallel()

	dstDir := t.TempDir()
	missingManifest := filepath.Join(t.TempDir(), "missing.gz")

	cases := map[string][]string{
		"no args":         {},
		"unknown command": {"bogus"},
		"save bad flag":   {"save", "--bad-flag", dstDir, missingManifest},
		"save missing":    {"save", dstDir},
		"apply bad flag":  {"apply", "--bad-flag", dstDir, missingManifest},
		"apply missing":   {"apply", dstDir, missingManifest},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, run(args))
		})
	}
}

func TestRun_SaveMountRootSkipsRootLostFoundOnly(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstFromRoot := t.TempDir()
	dstFromSub := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(srcDir, "lost+found"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lost+found", "orphan"), []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub", "lost+found"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "keep.txt"), []byte("ok"), 0o600))

	rootManifest := filepath.Join(t.TempDir(), "root.gz")
	require.NoError(t, run([]string{"save", "--mount-root", srcDir, srcDir, rootManifest}))
	require.NoError(t, run([]string{"apply", "--no-chown", "--mount-root", dstFromRoot, dstFromRoot, rootManifest}))

	assert.NoDirExists(t, filepath.Join(dstFromRoot, "lost+found"))
	assert.DirExists(t, filepath.Join(dstFromRoot, "sub", "lost+found"))

	subManifest := filepath.Join(t.TempDir(), "sub.gz")
	require.NoError(t, run([]string{"save", "--mount-root", srcDir, filepath.Join(srcDir, "sub"), subManifest}))
	require.NoError(t, run([]string{"apply", "--no-chown", "--mount-root", dstFromSub, dstFromSub, subManifest}))

	assert.DirExists(t, filepath.Join(dstFromSub, "lost+found"))
}
