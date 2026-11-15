package posixmeta_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/posixmeta"
)

func TestSaveApply_RoundTrip(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	outsideDir := t.TempDir()

	t.Cleanup(func() { restoreWritableTree(srcDir); restoreWritableTree(dstDir) })

	outsideFile := filepath.Join(outsideDir, "canary.txt")
	outsideTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	writeTestFile(t, outsideFile, []byte("untouched"), 0o644)
	require.NoError(t, os.Chtimes(outsideFile, outsideTime, outsideTime))

	fileTime := time.Date(2024, 6, 1, 12, 30, 45, 0, time.UTC)
	dirTime := time.Date(2024, 5, 1, 8, 15, 0, 0, time.UTC)

	seedSourceTree(t, srcDir, outsideFile, fileTime, dirTime)

	setuidSrcInfo, err := os.Lstat(filepath.Join(srcDir, "setuid.bin"))
	require.NoError(t, err)

	wantSetuidMode := setuidSrcInfo.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)

	manifestPath := filepath.Join(t.TempDir(), "posix.gz")
	require.NoError(t, posixmeta.Save(srcDir, manifestPath, posixmeta.SaveOptions{}))

	// Simulate what rclone --links leaves in the destination before posixmeta apply:
	// files exist with default modes/timestamps, empty dirs may be absent, symlinks exist.
	seedRestoredContent(t, dstDir, outsideFile)

	// Make a directory read-only in the source only AFTER saving to restore cleanly,
	// then re-run Apply a second time to verify idempotency on an already 0500 tree.
	opts := posixmeta.ApplyOptions{MountRoot: dstDir, NoChown: os.Geteuid() != 0}
	require.NoError(t, posixmeta.Apply(dstDir, manifestPath, opts))
	assertRestoredTree(t, dstDir, fileTime, dirTime, wantSetuidMode)

	// Re-apply onto the already restricted destination tree.
	require.NoError(t, posixmeta.Apply(dstDir, manifestPath, opts))
	assertRestoredTree(t, dstDir, fileTime, dirTime, wantSetuidMode)

	outsideInfo, err := os.Lstat(outsideFile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), outsideInfo.Mode())
	assert.True(t, outsideInfo.ModTime().Equal(outsideTime))
}

func seedSourceTree(t *testing.T, srcDir, outsideFile string, fileTime, dirTime time.Time) {
	t.Helper()

	writeTestFile(t, filepath.Join(srcDir, "secret.txt"), []byte("secret"), 0o600)
	writeTestFile(t, filepath.Join(srcDir, "run.sh"), []byte("#!/bin/sh\n"), 0o750)
	writeTestFile(t, filepath.Join(srcDir, "setuid.bin"), []byte("bin"), 0o755|fs.ModeSetuid)

	// Special filename characters: spaces, tabs, quotes, newlines, Unicode.
	weirdName := "weird 'name'\t\"line1\nline2\" café.txt"
	writeTestFile(t, filepath.Join(srcDir, weirdName), []byte("data"), 0o640)

	emptyDir := filepath.Join(srcDir, "empty-sticky")
	require.NoError(t, os.Mkdir(emptyDir, 0o777))
	require.NoError(t, os.Chmod(emptyDir, 0o777|fs.ModeSticky))

	roParent := filepath.Join(srcDir, "ro-parent")
	roChild := filepath.Join(roParent, "ro-empty")
	require.NoError(t, os.MkdirAll(roChild, 0o700))
	writeTestFile(t, filepath.Join(roParent, "ro-file.txt"), []byte("ro"), 0o400)

	require.NoError(t, os.Symlink("secret.txt", filepath.Join(srcDir, "link-valid")))
	require.NoError(t, os.Symlink("does-not-exist", filepath.Join(srcDir, "link-dangling")))
	require.NoError(t, os.Symlink(outsideFile, filepath.Join(srcDir, "link-outside")))

	for _, rel := range []string{"secret.txt", "run.sh", "setuid.bin", weirdName, "ro-parent/ro-file.txt"} {
		require.NoError(t, os.Chtimes(filepath.Join(srcDir, rel), fileTime, fileTime))
	}

	require.NoError(t, os.Chtimes(emptyDir, dirTime, dirTime))
	require.NoError(t, os.Chtimes(roChild, dirTime, dirTime))
	require.NoError(t, os.Chmod(roParent, 0o500))
	require.NoError(t, os.Chtimes(roParent, dirTime, dirTime))
}

func seedRestoredContent(t *testing.T, dstDir, outsideFile string) {
	t.Helper()

	writeTestFile(t, filepath.Join(dstDir, "secret.txt"), []byte("secret"), 0o644)
	writeTestFile(t, filepath.Join(dstDir, "run.sh"), []byte("#!/bin/sh\n"), 0o644)
	writeTestFile(t, filepath.Join(dstDir, "setuid.bin"), []byte("bin"), 0o644)

	weirdName := "weird 'name'\t\"line1\nline2\" café.txt"
	writeTestFile(t, filepath.Join(dstDir, weirdName), []byte("data"), 0o644)

	roParent := filepath.Join(dstDir, "ro-parent")
	require.NoError(t, os.Mkdir(roParent, 0o755))
	writeTestFile(t, filepath.Join(roParent, "ro-file.txt"), []byte("ro"), 0o644)

	require.NoError(t, os.Symlink("secret.txt", filepath.Join(dstDir, "link-valid")))
	require.NoError(t, os.Symlink("does-not-exist", filepath.Join(dstDir, "link-dangling")))
	require.NoError(t, os.Symlink(outsideFile, filepath.Join(dstDir, "link-outside")))
}

func assertRestoredTree(t *testing.T, dstDir string, fileTime, dirTime time.Time, wantSetuidMode fs.FileMode) {
	t.Helper()

	info, err := os.Lstat(filepath.Join(dstDir, "secret.txt"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
	assert.True(t, info.ModTime().Equal(fileTime))

	info, err = os.Lstat(filepath.Join(dstDir, "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o750), info.Mode().Perm())

	info, err = os.Lstat(filepath.Join(dstDir, "setuid.bin"))
	require.NoError(t, err)
	assert.Equal(
		t,
		wantSetuidMode,
		info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky),
	)

	weirdName := "weird 'name'\t\"line1\nline2\" café.txt"
	info, err = os.Lstat(filepath.Join(dstDir, weirdName))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o640), info.Mode().Perm())

	info, err = os.Lstat(filepath.Join(dstDir, "empty-sticky"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, fs.FileMode(0o777)|fs.ModeSticky, info.Mode()&(fs.ModePerm|fs.ModeSticky))
	assert.True(t, info.ModTime().Equal(dirTime))

	roParentInfo, err := os.Lstat(filepath.Join(dstDir, "ro-parent"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o500), roParentInfo.Mode().Perm())
	assert.True(t, roParentInfo.ModTime().Equal(dirTime))

	roChildInfo, err := os.Lstat(filepath.Join(dstDir, "ro-parent", "ro-empty"))
	require.NoError(t, err)
	assert.True(t, roChildInfo.IsDir())
	assert.Equal(t, fs.FileMode(0o700), roChildInfo.Mode().Perm())
	assert.True(t, roChildInfo.ModTime().Equal(dirTime))

	roFileInfo, err := os.Lstat(filepath.Join(dstDir, "ro-parent", "ro-file.txt"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o400), roFileInfo.Mode().Perm())
	assert.True(t, roFileInfo.ModTime().Equal(fileTime))

	for _, linkName := range []string{"link-valid", "link-dangling", "link-outside"} {
		linkInfo, linkErr := os.Lstat(filepath.Join(dstDir, linkName))
		require.NoError(t, linkErr)
		assert.NotZero(t, linkInfo.Mode()&fs.ModeSymlink, "%s should remain a symlink", linkName)
	}
}

func TestSaveApply_OutOfInt64NanoRangeModTimes(t *testing.T) {
	t.Parallel()

	ancient := time.Date(1601, 1, 1, 0, 0, 0, 123456789, time.UTC)
	future := time.Date(2300, 1, 1, 0, 0, 0, 987654321, time.UTC)

	for _, want := range []time.Time{ancient, future} {
		data, err := json.Marshal(posixmeta.Entry{
			Path: "f", Type: posixmeta.TypeFile, Mode: 0o644, ModTime: want,
		})
		require.NoError(t, err)

		var decoded posixmeta.Entry

		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.True(t, decoded.ModTime.Equal(want), "json round-trip lost %v -> %v", want, decoded.ModTime)
	}

	srcDir, dstDir := t.TempDir(), t.TempDir()
	for name, ts := range map[string]time.Time{"ancient.txt": ancient, "future.txt": future} {
		srcPath := filepath.Join(srcDir, name)
		writeTestFile(t, srcPath, []byte(name), 0o600)
		require.NoError(t, os.Chtimes(srcPath, ts, ts))
		writeTestFile(t, filepath.Join(dstDir, name), []byte(name), 0o644)
	}

	manifestPath := filepath.Join(t.TempDir(), "posix.gz")
	require.NoError(t, posixmeta.Save(srcDir, manifestPath, posixmeta.SaveOptions{}))
	require.NoError(t, posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: dstDir,
		NoChown:   true,
	}))

	for _, name := range []string{"ancient.txt", "future.txt"} {
		srcInfo, err := os.Lstat(filepath.Join(srcDir, name))
		require.NoError(t, err)

		dstInfo, err := os.Lstat(filepath.Join(dstDir, name))
		require.NoError(t, err)
		assert.True(t, dstInfo.ModTime().Equal(srcInfo.ModTime()),
			"%s mtime got %v want %v", name, dstInfo.ModTime(), srcInfo.ModTime())
	}
}

func TestSave_RejectsReservedManifestFile(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, posixmeta.ManifestFileName), []byte("conflict"), 0o600))

	err := posixmeta.Save(srcDir, filepath.Join(t.TempDir(), "posix.gz"), posixmeta.SaveOptions{})
	require.ErrorContains(t, err, posixmeta.ManifestFileName)
}

func TestApply_NonRootSkipsDirTimesAndKeepsFileTimes(t *testing.T) {
	t.Parallel()

	fileTime := time.Date(2024, 1, 10, 9, 8, 7, 0, time.UTC)
	oldDirTime := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	manifestPath := writeManifest(t, []posixmeta.Entry{
		{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755, ModTime: oldDirTime},
		{Path: "dir", Type: posixmeta.TypeDir, Mode: 0o750, ModTime: oldDirTime},
		{Path: "dir/file.txt", Type: posixmeta.TypeFile, Mode: 0o600, ModTime: fileTime},
	})

	dstDir := t.TempDir()
	subDir := filepath.Join(dstDir, "dir")
	filePath := filepath.Join(subDir, "file.txt")
	require.NoError(t, os.Mkdir(subDir, 0o755))
	writeTestFile(t, filePath, []byte("hi"), 0o644)

	beforeSubInfo, err := os.Lstat(subDir)
	require.NoError(t, err)

	err = posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: dstDir,
		NonRoot:   true,
	})
	require.NoError(t, err)

	subInfo, err := os.Lstat(subDir)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o750), subInfo.Mode().Perm())
	assert.False(t, subInfo.ModTime().Equal(oldDirTime), "non-root must omit directory mtime")
	assert.True(t, subInfo.ModTime().Equal(beforeSubInfo.ModTime()))

	fileInfo, err := os.Lstat(filePath)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), fileInfo.Mode().Perm())
	assert.True(t, fileInfo.ModTime().Equal(fileTime), "non-root should still restore file mtime")
}

func TestApply_SkipsMissingFilesAndCreatesMountSubpath(t *testing.T) {
	t.Parallel()

	mountRoot := t.TempDir()
	targetDir := filepath.Join(mountRoot, "nested", "subpath")

	manifestPath := writeManifest(t, []posixmeta.Entry{
		{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755},
		{Path: "empty-sub", Type: posixmeta.TypeDir, Mode: 0o700},
		{Path: "excluded.txt", Type: posixmeta.TypeFile, Mode: 0o600},
	})

	err := posixmeta.Apply(targetDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: mountRoot,
		NoChown:   true,
	})
	require.NoError(t, err)

	emptyInfo, err := os.Lstat(filepath.Join(targetDir, "empty-sub"))
	require.NoError(t, err)
	assert.True(t, emptyInfo.IsDir())
	assert.Equal(t, fs.FileMode(0o700), emptyInfo.Mode().Perm())
}

func TestApply_NoChownSkipsLchown(t *testing.T) {
	t.Parallel()

	dstDir := t.TempDir()
	filePath := filepath.Join(dstDir, "f.txt")
	writeTestFile(t, filePath, []byte("x"), 0o644)

	// Invalid huge UID/GID would fail Lchown even for root if NoChown were ignored.
	manifestPath := writeManifest(t, []posixmeta.Entry{
		{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755, UID: 999999999, GID: 999999999},
		{Path: "f.txt", Type: posixmeta.TypeFile, Mode: 0o600, UID: 999999999, GID: 999999999},
	})

	err := posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: dstDir,
		NoChown:   true,
	})
	require.NoError(t, err)

	info, err := os.Lstat(filePath)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

func TestApply_RejectsInvalidManifestEntries(t *testing.T) {
	t.Parallel()

	tests := map[string]posixmeta.Entry{
		"empty path":       {Path: "", Type: posixmeta.TypeFile},
		"absolute path":    {Path: "/etc/passwd", Type: posixmeta.TypeFile},
		"trailing slash":   {Path: "dir/", Type: posixmeta.TypeDir},
		"parent traversal": {Path: "../escape", Type: posixmeta.TypeFile},
		"nested traversal": {Path: "a/../../escape", Type: posixmeta.TypeFile},
		"nul byte":         {Path: "a\x00b", Type: posixmeta.TypeFile},
		"root as file":     {Path: ".", Type: posixmeta.TypeFile},
		"unknown type":     {Path: "fifo", Type: "fifo"},
		"invalid mode":     {Path: "file", Type: posixmeta.TypeFile, Mode: 0o10000},
		"negative uid":     {Path: "file", Type: posixmeta.TypeFile, UID: -1},
		"reserved name":    {Path: posixmeta.ManifestFileName, Type: posixmeta.TypeFile},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entries := []posixmeta.Entry{entry}
			if entry.Path != "." {
				entries = []posixmeta.Entry{{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755}, entry}
			}

			manifestPath := writeManifest(t, entries)
			dstDir := t.TempDir()

			err := posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{NoChown: true})
			require.Error(t, err)
		})
	}
}

func TestApply_RejectsEmptyOrRootlessManifest(t *testing.T) {
	t.Parallel()

	t.Run("empty manifest", func(t *testing.T) {
		t.Parallel()

		manifestPath := writeManifest(t, nil)
		err := posixmeta.Apply(t.TempDir(), manifestPath, posixmeta.ApplyOptions{NoChown: true})
		require.ErrorContains(t, err, "missing root")
	})

	t.Run("missing root entry", func(t *testing.T) {
		t.Parallel()

		manifestPath := writeManifest(t, []posixmeta.Entry{
			{Path: "only-file.txt", Type: posixmeta.TypeFile, Mode: 0o600},
		})
		err := posixmeta.Apply(t.TempDir(), manifestPath, posixmeta.ApplyOptions{NoChown: true})
		require.ErrorContains(t, err, "missing root")
	})
}

func TestApply_RejectsCorruptAndEscapingInputs(t *testing.T) {
	t.Parallel()

	t.Run("corrupt gzip", func(t *testing.T) {
		t.Parallel()

		corruptPath := filepath.Join(t.TempDir(), "bad.gz")
		require.NoError(t, os.WriteFile(corruptPath, []byte("not gzip"), 0o600))

		err := posixmeta.Apply(t.TempDir(), corruptPath, posixmeta.ApplyOptions{NoChown: true})
		require.Error(t, err)
	})

	t.Run("target escapes mount root", func(t *testing.T) {
		t.Parallel()

		manifestPath := writeManifest(t, []posixmeta.Entry{{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755}})
		mountRoot := t.TempDir()
		outsideTarget := t.TempDir()

		err := posixmeta.Apply(outsideTarget, manifestPath, posixmeta.ApplyOptions{
			MountRoot: mountRoot,
			NoChown:   true,
		})
		require.ErrorContains(t, err, "escapes mount root")
	})
}

func TestApply_AtomicValidationLeavesTargetUntouched(t *testing.T) {
	t.Parallel()

	dstDir := t.TempDir()
	keepFile := filepath.Join(dstDir, "keep.txt")
	writeTestFile(t, keepFile, []byte("keep"), 0o644)

	manifestPath := writeManifest(t, []posixmeta.Entry{
		{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755},
		{Path: "keep.txt", Type: posixmeta.TypeFile, Mode: 0o600},
		{Path: "../escape", Type: posixmeta.TypeFile, Mode: 0o600},
	})

	err := posixmeta.Apply(dstDir, manifestPath, posixmeta.ApplyOptions{
		MountRoot: dstDir,
		NoChown:   true,
	})
	require.Error(t, err)

	info, err := os.Lstat(keepFile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), info.Mode().Perm())
}

func TestSaveApply_InRootSymlinkSubpathAndEscapingSymlink(t *testing.T) {
	t.Parallel()

	t.Run("follows in-root symlink subpath", func(t *testing.T) {
		t.Parallel()

		srcMount, dstMount := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(srcMount, "releases", "v2", "sub"), 0o755))
		writeTestFile(t, filepath.Join(srcMount, "releases", "v2", "sub", "app.txt"), []byte("v2"), 0o600)
		require.NoError(t, os.Symlink("releases/v2", filepath.Join(srcMount, "current")))

		manifestPath := filepath.Join(t.TempDir(), "posix.gz")
		require.NoError(t, posixmeta.Save(
			filepath.Join(srcMount, "current", "sub"), manifestPath, posixmeta.SaveOptions{MountRoot: srcMount}))

		require.NoError(t, os.MkdirAll(filepath.Join(dstMount, "releases", "v2", "sub"), 0o755))
		writeTestFile(t, filepath.Join(dstMount, "releases", "v2", "sub", "app.txt"), []byte("v2"), 0o644)
		require.NoError(t, os.Symlink("releases/v2", filepath.Join(dstMount, "current")))

		require.NoError(t, posixmeta.Apply(filepath.Join(dstMount, "current", "sub"), manifestPath,
			posixmeta.ApplyOptions{MountRoot: dstMount, NoChown: true}))
		assertModePerm(t, filepath.Join(dstMount, "releases", "v2", "sub", "app.txt"), 0o600)
	})

	t.Run("rejects escaping symlink on apply", func(t *testing.T) {
		t.Parallel()

		dstDir, outsideDir := t.TempDir(), t.TempDir()
		victim := filepath.Join(outsideDir, "victim.txt")
		writeTestFile(t, victim, []byte("keep"), 0o400)
		require.NoError(t, os.Symlink(outsideDir, filepath.Join(dstDir, "escape")))

		for _, entry := range []posixmeta.Entry{
			{Path: "escape/victim.txt", Type: posixmeta.TypeFile, Mode: 0o777},
			{Path: "escape/newdir", Type: posixmeta.TypeDir, Mode: 0o755},
		} {
			manifest := writeManifest(t, []posixmeta.Entry{{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755}, entry})
			require.Error(
				t,
				posixmeta.Apply(dstDir, manifest, posixmeta.ApplyOptions{MountRoot: dstDir, NoChown: true}),
			)
		}

		assertModePerm(t, victim, 0o400)
		assert.NoDirExists(t, filepath.Join(outsideDir, "newdir"))
	})
}

func TestApply_ChownPermissionErrorSuggestsNoChown(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("requires non-root user")
	}

	dstDir := t.TempDir()
	writeTestFile(t, filepath.Join(dstDir, "f.txt"), []byte("x"), 0o644)
	manifest := writeManifest(t, []posixmeta.Entry{
		{Path: ".", Type: posixmeta.TypeDir, Mode: 0o755, UID: 0, GID: 0},
		{Path: "f.txt", Type: posixmeta.TypeFile, Mode: 0o600, UID: 0, GID: 0},
	})
	err := posixmeta.Apply(dstDir, manifest, posixmeta.ApplyOptions{MountRoot: dstDir})
	require.ErrorContains(t, err, "--no-chown")
}

func TestSave_MountRootSkipsRootLostFoundAndRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()

	mountDir := t.TempDir()
	outsideDir := t.TempDir()
	rootLostFound := filepath.Join(mountDir, "lost+found")

	t.Cleanup(func() {
		_ = os.Chmod(rootLostFound, 0o700)

		restoreWritableTree(mountDir)
	})

	require.NoError(t, os.Mkdir(rootLostFound, 0o700))
	writeTestFile(t, filepath.Join(rootLostFound, "orphan.txt"), []byte("orphan"), 0o600)
	require.NoError(t, os.Chmod(rootLostFound, 0o000))

	subLostFound := filepath.Join(mountDir, "sub", "lost+found")
	require.NoError(t, os.MkdirAll(subLostFound, 0o750))
	writeTestFile(t, filepath.Join(subLostFound, "nested.txt"), []byte("nested"), 0o640)
	writeTestFile(t, filepath.Join(mountDir, "secret.txt"), []byte("secret"), 0o600)
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(mountDir, "link-out")))

	rootManifest := filepath.Join(t.TempDir(), "root.gz")
	require.NoError(t, posixmeta.Save(mountDir, rootManifest, posixmeta.SaveOptions{
		MountRoot: mountDir,
	}))

	rootPaths := readManifestPaths(t, rootManifest)
	assert.NotContains(t, rootPaths, "lost+found")
	assert.NotContains(t, rootPaths, "lost+found/orphan.txt")
	assert.Contains(t, rootPaths, "sub/lost+found")
	assert.Contains(t, rootPaths, "sub/lost+found/nested.txt")
	assert.Contains(t, rootPaths, "secret.txt")

	subManifest := filepath.Join(t.TempDir(), "sub.gz")
	require.NoError(t, posixmeta.Save(
		filepath.Join(mountDir, "sub"),
		subManifest,
		posixmeta.SaveOptions{MountRoot: mountDir},
	))

	subPaths := readManifestPaths(t, subManifest)
	assert.Contains(t, subPaths, "lost+found")
	assert.Contains(t, subPaths, "lost+found/nested.txt")

	linkManifest := filepath.Join(t.TempDir(), "link.gz")
	err := posixmeta.Save(
		filepath.Join(mountDir, "link-out"),
		linkManifest,
		posixmeta.SaveOptions{MountRoot: mountDir},
	)
	require.Error(t, err)
	assert.NoFileExists(t, linkManifest)
}

func readManifestPaths(t *testing.T, manifestPath string) []string {
	t.Helper()

	file, err := os.Open(manifestPath)
	require.NoError(t, err)

	defer func() { _ = file.Close() }()

	gzReader, err := gzip.NewReader(file)
	require.NoError(t, err)

	defer func() { _ = gzReader.Close() }()

	decoder := json.NewDecoder(gzReader)

	var paths []string

	for {
		var entry posixmeta.Entry

		decodeErr := decoder.Decode(&entry)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		require.NoError(t, decodeErr)

		paths = append(paths, entry.Path)
	}

	return paths
}

func assertModePerm(t *testing.T, filePath string, want fs.FileMode) {
	t.Helper()

	info, err := os.Lstat(filePath)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm())
}

func writeTestFile(t *testing.T, filePath string, content []byte, perm fs.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(filePath, content, 0o600))

	if perm != 0o600 {
		require.NoError(t, os.Chmod(filePath, perm))
	}
}

func writeManifest(t *testing.T, entries []posixmeta.Entry) string {
	t.Helper()

	var buf bytes.Buffer

	gzWriter := gzip.NewWriter(&buf)
	encoder := json.NewEncoder(gzWriter)

	for _, entry := range entries {
		require.NoError(t, encoder.Encode(entry))
	}

	require.NoError(t, gzWriter.Close())

	path := filepath.Join(t.TempDir(), "posix.gz")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	return path
}

func restoreWritableTree(rootDir string) {
	if parentRoot, err := os.OpenRoot(filepath.Dir(rootDir)); err == nil {
		_ = parentRoot.Chmod(filepath.Base(rootDir), 0o755)
		_ = parentRoot.Close()
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return
	}

	defer func() { _ = root.Close() }()

	_ = fs.WalkDir(root.FS(), ".", func(relPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if dirEntry.IsDir() {
			_ = root.Chmod(relPath, 0o755)
		}

		return nil
	})
}
