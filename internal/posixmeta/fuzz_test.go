package posixmeta_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/posixmeta"
)

const fuzzBaseUnixSec = int64(1700000000)

func FuzzApplyManifest(f *testing.F) {
	addApplyManifestSeeds(f)

	f.Fuzz(func(t *testing.T, rawStream bool, payload []byte) {
		outer := t.TempDir()
		t.Cleanup(func() { restoreWritableTree(outer) })

		fixedTime := time.Unix(fuzzBaseUnixSec, 0).UTC()
		canary, outsideVictim, absVictim := seedOutsideCanaries(t, outer, fixedTime)
		mountRoot := seedMountTrapTree(t, outer, fixedTime)

		manifestPath := writeFuzzManifest(t, outer, rawStream, payload)

		_ = posixmeta.Apply(mountRoot, manifestPath, posixmeta.ApplyOptions{
			MountRoot: mountRoot,
			NoChown:   true,
		})

		assertApplyInvariants(t, applyInvariantSpec{
			outerDir:      outer,
			canary:        canary,
			outsideVictim: outsideVictim,
			absVictim:     absVictim,
			fixedTime:     fixedTime,
		})
	})
}

func addApplyManifestSeeds(f *testing.F) {
	f.Helper()

	for _, payload := range [][]byte{
		[]byte(
			`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"2023-11-14T22:13:20Z"}` + "\n" +
				`{"path":"sub","type":"dir","mode":448,"uid":0,"gid":0,"mtime":"2023-11-14T22:13:20Z"}` + "\n" +
				`{"path":"protected/secret.txt","type":"file","mode":256,"uid":0,"gid":0,` +
				`"mtime":"2023-11-14T22:13:20Z"}` + "\n",
		),
		[]byte(`{"path":"../canary.txt","type":"file","mode":511,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(
			`{"path":"a/../../canary.txt","type":"file","mode":511,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n",
		),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-outside-file","type":"file","mode":511,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-outside-dir/victim.txt","type":"file","mode":511,"uid":0,"gid":0,` +
			`"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-outside-dir/newdir","type":"dir","mode":511,"uid":0,"gid":0,` +
			`"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-abs/victim2.txt","type":"file","mode":511,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-inroot-dir/secret.txt","type":"file","mode":511,"uid":0,"gid":0,` +
			`"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-inroot-dir/injected","type":"dir","mode":511,"uid":0,"gid":0,` +
			`"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n" +
			`{"path":"link-inroot-file","type":"file","mode":511,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":"a/./b","type":"file","mode":420,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":"a//b","type":"file","mode":420,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".pv-migrate-posix.gz","type":"file","mode":420,"uid":0,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` +
			"\n"),
		[]byte(`{"path":"f","type":"file","mode":4096,"uid":-1,"gid":0,"mtime":"1970-01-01T00:00:00Z"}` + "\n"),
		[]byte(`{"path":".","type":"dir","mode":493,"uid":0,"gid":0,"mtime":"not-a-timestamp"}` + "\n"),
		[]byte("{\"pAth\":\".\",\"tYpe\":\"dir\"} "),
		[]byte("not json\n"),
		[]byte(""),
	} {
		f.Add(false, payload)
	}

	f.Add(true, []byte("corrupt gzip stream"))
}

func seedOutsideCanaries(t *testing.T, outer string, fixedTime time.Time) (string, string, string) {
	t.Helper()

	canary := filepath.Join(outer, "canary.txt")
	outsideDir := filepath.Join(outer, "outside-dir")
	absDir := filepath.Join(outer, "abs-target")

	for _, dirPath := range []string{outsideDir, absDir} {
		require.NoError(t, os.MkdirAll(dirPath, 0o750))
	}

	outsideVictim := filepath.Join(outsideDir, "victim.txt")
	absVictim := filepath.Join(absDir, "victim2.txt")

	for _, filePath := range []string{canary, outsideVictim, absVictim} {
		require.NoError(t, os.WriteFile(filePath, []byte("safe"), 0o600))
		require.NoError(t, os.Chtimes(filePath, fixedTime, fixedTime))
	}

	return canary, outsideVictim, absVictim
}

func seedMountTrapTree(t *testing.T, outer string, fixedTime time.Time) string {
	t.Helper()

	mountRoot := filepath.Join(outer, "mount")
	protectedDir := filepath.Join(mountRoot, "protected")
	require.NoError(t, os.MkdirAll(protectedDir, 0o750))

	protectedFile := filepath.Join(protectedDir, "secret.txt")
	writeTestFile(t, protectedFile, []byte("secret"), 0o400)
	require.NoError(t, os.Chtimes(protectedFile, fixedTime, fixedTime))

	require.NoError(t, os.Symlink("../canary.txt", filepath.Join(mountRoot, "link-outside-file")))
	require.NoError(t, os.Symlink("../outside-dir", filepath.Join(mountRoot, "link-outside-dir")))
	require.NoError(t, os.Symlink(filepath.Join(outer, "abs-target"), filepath.Join(mountRoot, "link-abs")))
	require.NoError(t, os.Symlink("protected", filepath.Join(mountRoot, "link-inroot-dir")))
	require.NoError(t, os.Symlink("protected/secret.txt", filepath.Join(mountRoot, "link-inroot-file")))

	return mountRoot
}

func writeFuzzManifest(t *testing.T, outer string, rawStream bool, payload []byte) string {
	t.Helper()

	manifestPath := filepath.Join(outer, "manifest.gz")
	data := payload

	if !rawStream {
		var compressed bytes.Buffer

		gzWriter := gzip.NewWriter(&compressed)
		_, err := gzWriter.Write(payload)
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())

		data = compressed.Bytes()
	}

	require.NoError(t, os.WriteFile(manifestPath, data, 0o600))

	return manifestPath
}

type applyInvariantSpec struct {
	outerDir      string
	canary        string
	outsideVictim string
	absVictim     string
	fixedTime     time.Time
}

func assertApplyInvariants(t *testing.T, spec applyInvariantSpec) {
	t.Helper()

	for _, filePath := range []string{spec.canary, spec.outsideVictim, spec.absVictim} {
		assertUnchangedFile(t, filePath, fs.FileMode(0o600), spec.fixedTime)
	}

	assertDirEntries(t, spec.outerDir, []string{"abs-target", "canary.txt", "manifest.gz", "mount", "outside-dir"})
	assertDirEntries(t, filepath.Join(spec.outerDir, "outside-dir"), []string{"victim.txt"})
	assertDirEntries(t, filepath.Join(spec.outerDir, "abs-target"), []string{"victim2.txt"})
}

func assertUnchangedFile(t *testing.T, filePath string, wantPerm fs.FileMode, wantTime time.Time) {
	t.Helper()

	info, err := os.Lstat(filePath)
	require.NoError(t, err)
	assertEqualModeAndTime(t, filePath, info, wantPerm, wantTime)
}

func assertEqualModeAndTime(
	t *testing.T,
	label string,
	info fs.FileInfo,
	wantPerm fs.FileMode,
	wantTime time.Time,
) {
	t.Helper()

	require.Equal(t, wantPerm, info.Mode().Perm(), "unexpected mode for %s", label)
	require.True(t, info.ModTime().Equal(wantTime), "unexpected mtime for %s: got %v want %v",
		label, info.ModTime(), wantTime)
}

func assertDirEntries(t *testing.T, dirPath string, wantNames []string) {
	t.Helper()

	entries, err := os.ReadDir(dirPath)
	require.NoError(t, err)

	gotNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		gotNames = append(gotNames, entry.Name())
	}

	slices.Sort(gotNames)
	slices.Sort(wantNames)
	require.Equal(t, wantNames, gotNames, "unexpected entries in %s", dirPath)
}

func FuzzSaveApplyRoundTrip(f *testing.F) {
	f.Add([]byte("weird 'name'\t\"line1\nline2\"\x00café\x00naïve-€\x00.hidden\x00..dot"), []byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte("dirA\x00dirB\x00dirC\x00leaf.txt"), []byte{0, 0, 0, 1, 12, 34})
	f.Add([]byte("ro-parent\x00ro-empty\x00secret.txt\x00run.sh"), []byte{0, 0, 1, 3, 7, 9})
	f.Add([]byte(""), []byte(""))

	f.Fuzz(func(t *testing.T, names, ops []byte) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()
		t.Cleanup(func() { restoreWritableTree(srcDir); restoreWritableTree(dstDir) })

		seedRandomTree(srcDir, names, ops)
		wantTree := snapshotTree(t, srcDir)

		manifestPath := filepath.Join(t.TempDir(), "posix.gz")
		require.NoError(t, posixmeta.Save(srcDir, manifestPath, posixmeta.SaveOptions{}))

		populateRestoredFilesAndLinks(t, srcDir, dstDir)

		opts := posixmeta.ApplyOptions{MountRoot: dstDir, NoChown: os.Geteuid() != 0}
		require.NoError(t, posixmeta.Apply(dstDir, manifestPath, opts))
		require.NoError(t, posixmeta.Apply(dstDir, manifestPath, opts))

		gotTree := snapshotTree(t, dstDir)
		require.Equal(t, wantTree, gotTree)
	})
}

type seededMeta struct {
	relPath string
	mode    fs.FileMode
	mtime   time.Time
	isDir   bool
}

func seedRandomTree(srcDir string, names, ops []byte) {
	segments := extractSegments(names)
	dirRelPaths := []string{"."}

	var postAttrs []seededMeta

	for idx, seg := range segments {
		opKind := byteAt(ops, idx*3) % 4
		opMode := byteAt(ops, idx*3+1)
		opTime := byteAt(ops, idx*3+2)

		parentRel := dirRelPaths[int(byteAt(ops, idx*3+3))%len(dirRelPaths)]
		relPath := seg

		if parentRel != "." {
			relPath = filepath.ToSlash(filepath.Join(parentRel, seg))
		}

		fullPath := filepath.Join(srcDir, filepath.FromSlash(relPath))
		if _, err := os.Lstat(fullPath); !errors.Is(err, fs.ErrNotExist) {
			continue
		}

		mtime := time.Unix(fuzzBaseUnixSec+int64(opTime)*3600, int64(opTime)*1_000_000).UTC()
		created, isDir, isLink := createFuzzedEntry(fullPath, relPath, opKind, dirRelPaths)

		if !created {
			continue
		}

		if isDir {
			dirRelPaths = append(dirRelPaths, relPath)
		}

		if isLink {
			continue
		}

		postAttrs = append(postAttrs, seededMeta{
			relPath: relPath,
			mode:    buildFuzzedMode(opMode, isDir),
			mtime:   mtime,
			isDir:   isDir,
		})
	}

	applySeededAttrsBottomUp(srcDir, postAttrs)
}

func extractSegments(raw []byte) []string {
	const maxSegments = 10

	var segments []string

	for part := range bytes.SplitSeq(raw, []byte{0}) {
		if len(segments) >= maxSegments {
			break
		}

		seg := sanitizeSegment(part)
		if seg != "" {
			segments = append(segments, seg)
		}
	}

	return segments
}

func sanitizeSegment(part []byte) string {
	const maxSegBytes = 24

	if len(part) == 0 || !utf8.Valid(part) {
		return ""
	}

	seg := string(part)
	if len(seg) > maxSegBytes {
		seg = truncateUTF8(seg, maxSegBytes)
	}

	if seg == "" || seg == "." || seg == ".." || seg == posixmeta.ManifestFileName {
		return ""
	}

	if strings.ContainsRune(seg, '/') {
		return ""
	}

	return seg
}

func truncateUTF8(str string, maxBytes int) string {
	end := 0

	for idx := range str {
		if idx > maxBytes {
			break
		}

		end = idx
	}

	return str[:end]
}

func createFuzzedEntry(fullPath, relPath string, kind byte, existing []string) (bool, bool, bool) {
	switch kind {
	case 0:
		return os.Mkdir(fullPath, 0o750) == nil, true, false
	case 3:
		target := "dangling-target"
		if len(existing) > 1 {
			target = filepath.Base(existing[int(kind)%len(existing)])
		}

		if relPath == target {
			target = "dangling-target"
		}

		return os.Symlink(target, fullPath) == nil, false, true
	default:
		return os.WriteFile(fullPath, []byte("fuzz-data"), 0o600) == nil, false, false
	}
}

func buildFuzzedMode(raw byte, isDir bool) fs.FileMode {
	perm := fs.FileMode(raw) & 0o777

	if isDir && os.Geteuid() != 0 {
		perm |= 0o500
	}

	if raw&0x80 != 0 {
		if isDir {
			perm |= fs.ModeSticky | fs.ModeSetgid
		} else if raw&0x40 != 0 {
			perm |= fs.ModeSetuid
		}
	}

	return perm
}

func applySeededAttrsBottomUp(srcDir string, attrs []seededMeta) {
	slices.SortFunc(attrs, func(left, right seededMeta) int {
		return strings.Count(right.relPath, "/") - strings.Count(left.relPath, "/")
	})

	for _, item := range attrs {
		fullPath := filepath.Join(srcDir, filepath.FromSlash(item.relPath))
		if _, err := os.Lstat(fullPath); err != nil {
			continue
		}

		_ = os.Chtimes(fullPath, item.mtime, item.mtime)

		if err := os.Chmod(fullPath, item.mode); err != nil {
			_ = os.Chmod(fullPath, item.mode&0o777)
		}

		if item.isDir {
			// Setting mtime on a directory after chmod ensures its final timestamp holds.
			_ = os.Chtimes(fullPath, item.mtime, item.mtime)
		}
	}
}

func populateRestoredFilesAndLinks(t *testing.T, srcDir, dstDir string) {
	t.Helper()

	srcRoot, err := os.OpenRoot(srcDir)
	require.NoError(t, err)

	defer func() { _ = srcRoot.Close() }()

	dstRoot, err := os.OpenRoot(dstDir)
	require.NoError(t, err)

	defer func() { _ = dstRoot.Close() }()

	require.NoError(t, fs.WalkDir(srcRoot.FS(), ".", func(relPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil || relPath == "." || dirEntry.IsDir() {
			return walkErr
		}

		if parent := path.Dir(relPath); parent != "." {
			require.NoError(t, dstRoot.MkdirAll(parent, 0o750))
		}

		if dirEntry.Type()&fs.ModeSymlink != 0 {
			target, linkErr := srcRoot.Readlink(relPath)
			require.NoError(t, linkErr)

			return dstRoot.Symlink(target, relPath)
		}

		require.NoError(t, dstRoot.WriteFile(relPath, []byte("fuzz-data"), 0o600))
		require.NoError(t, dstRoot.Chmod(relPath, 0o644))

		return nil
	}))
}

type treeSnapshotEntry struct {
	kind       string
	mode       fs.FileMode
	modTime    time.Time
	linkTarget string
}

func snapshotTree(t *testing.T, rootDir string) map[string]treeSnapshotEntry {
	t.Helper()

	root, err := os.OpenRoot(rootDir)
	require.NoError(t, err)

	defer func() { _ = root.Close() }()

	result := make(map[string]treeSnapshotEntry)

	require.NoError(t, fs.WalkDir(root.FS(), ".", func(relPath string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, statErr := root.Lstat(relPath)
		require.NoError(t, statErr)

		result[relPath] = toSnapshotEntry(t, root, relPath, info)

		return nil
	}))

	return maps.Clone(result)
}

func toSnapshotEntry(t *testing.T, root *os.Root, relPath string, info fs.FileInfo) treeSnapshotEntry {
	t.Helper()

	modeBits := info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)

	switch {
	case info.IsDir():
		return treeSnapshotEntry{
			kind:    posixmeta.TypeDir,
			mode:    modeBits,
			modTime: info.ModTime().UTC(),
		}
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := root.Readlink(relPath)
		require.NoError(t, err)

		return treeSnapshotEntry{
			kind:       posixmeta.TypeLink,
			linkTarget: target,
		}
	default:
		return treeSnapshotEntry{
			kind:    posixmeta.TypeFile,
			mode:    modeBits,
			modTime: info.ModTime().UTC(),
		}
	}
}

func byteAt(data []byte, idx int) byte {
	if len(data) == 0 {
		return 0
	}

	return data[idx%len(data)]
}
