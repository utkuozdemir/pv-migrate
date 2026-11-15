package posixmeta

import (
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// ManifestFileName is the compressed metadata manifest stored alongside backed-up files.
	ManifestFileName = ".pv-migrate-posix.gz"

	// TypeDir identifies a directory entry in the manifest.
	TypeDir = "dir"
	// TypeFile identifies a regular file entry in the manifest.
	TypeFile = "file"
	// TypeLink identifies a symbolic link entry in the manifest.
	TypeLink = "link"

	specialModeMask = 0o7000
	fullModeMask    = 0o7777
	dirCreatePerm   = 0o755
	ownerRWXPerm    = 0o700
	manifestPerm    = 0o600
	setuidBit       = 0o4000
	setgidBit       = 0o2000
	stickyBit       = 0o1000
	lostFoundDir    = "lost+found"
)

// Entry describes the POSIX metadata of one directory tree entry relative to the backed-up root.
type Entry struct {
	Path    string    `json:"path"`
	Type    string    `json:"type"`
	Mode    uint32    `json:"mode"`
	UID     int       `json:"uid"`
	GID     int       `json:"gid"`
	ModTime time.Time `json:"mtime"`
}

// ApplyOptions configures how Apply restores metadata to the target directory.
type ApplyOptions struct {
	MountRoot string
	NoChown   bool
	NonRoot   bool
}

// SaveOptions configures how Save captures metadata from a source directory.
type SaveOptions struct {
	MountRoot string
}

// Save captures the POSIX metadata of rootDir using opts into a gzipped JSON Lines manifest at outPath.
func Save(rootDir, outPath string, opts SaveOptions) (retErr error) {
	root, skipLostFound, err := openSourceRoot(rootDir, opts.MountRoot)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	if _, statErr := root.Lstat(ManifestFileName); statErr == nil {
		return fmt.Errorf("volume root contains reserved file %s", ManifestFileName)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("failed to check reserved file %s: %w", ManifestFileName, statErr)
	}

	outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, manifestPerm)
	if err != nil {
		return fmt.Errorf("failed to create manifest %q: %w", outPath, err)
	}

	gzWriter := gzip.NewWriter(outFile)

	defer func() {
		if retErr != nil {
			_ = gzWriter.Close()
			_ = outFile.Close()
			_ = os.Remove(outPath)
		}
	}()

	encoder := json.NewEncoder(gzWriter)
	if err := walkAndEncode(root, encoder, skipLostFound); err != nil {
		return err
	}

	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("failed to finalize gzip manifest %q: %w", outPath, err)
	}

	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close manifest %q: %w", outPath, err)
	}

	return nil
}

func walkAndEncode(root *os.Root, encoder *json.Encoder, skipLostFound bool) error {
	return fs.WalkDir(root.FS(), ".", func(relPath string, dirEntry fs.DirEntry, walkErr error) error {
		if skipLostFound && relPath == lostFoundDir && dirEntry != nil && dirEntry.IsDir() {
			return fs.SkipDir
		}

		if walkErr != nil {
			return walkErr
		}

		return encodePath(root, relPath, encoder)
	})
}

func encodePath(root *os.Root, relPath string, encoder *json.Encoder) error {
	info, err := root.Lstat(relPath)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", relPath, err)
	}

	entryType, ok := classifyEntry(info.Mode())
	if !ok {
		return nil
	}

	uid, gid := fileOwner(info)

	entry := Entry{
		Path:    relPath,
		Type:    entryType,
		Mode:    fileModeToUnix(info.Mode()),
		UID:     uid,
		GID:     gid,
		ModTime: info.ModTime().UTC(),
	}

	if err := encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to encode metadata for %q: %w", relPath, err)
	}

	return nil
}

// Apply restores POSIX metadata from the gzipped JSON Lines manifest at inPath to targetDir.
func Apply(targetDir, inPath string, opts ApplyOptions) error {
	dirs, err := readManifestDirs(inPath)
	if err != nil {
		return err
	}

	root, err := openTargetRoot(targetDir, opts.MountRoot)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	if err := ensureDirectories(root, dirs, opts); err != nil {
		return err
	}

	if err := applyManifestItems(root, inPath, opts); err != nil {
		return err
	}

	return applyDirectories(root, dirs, opts)
}

func readManifestDirs(inPath string) ([]Entry, error) {
	var (
		dirs    []Entry
		hasRoot bool
	)

	err := walkManifest(inPath, func(entry Entry) error {
		if entry.Path == "." {
			hasRoot = true
		}

		if entry.Type == TypeDir {
			dirs = append(dirs, entry)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if !hasRoot {
		return nil, errors.New("manifest is missing root directory entry")
	}

	return dirs, nil
}

func applyManifestItems(root *os.Root, inPath string, opts ApplyOptions) error {
	return walkManifest(inPath, func(entry Entry) error {
		if entry.Type == TypeDir {
			return nil
		}

		return applyItem(root, entry, opts)
	})
}

func walkManifest(inPath string, visit func(Entry) error) error {
	file, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("failed to open manifest %q: %w", inPath, err)
	}

	defer func() { _ = file.Close() }()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to open gzip manifest %q: %w", inPath, err)
	}

	defer func() { _ = gzReader.Close() }()

	if err := decodeAndVisitEntries(json.NewDecoder(gzReader), visit); err != nil {
		return err
	}

	if err := gzReader.Close(); err != nil {
		return fmt.Errorf("failed to read manifest trailer %q: %w", inPath, err)
	}

	return nil
}

func decodeAndVisitEntries(decoder *json.Decoder, visit func(Entry) error) error {
	for {
		var entry Entry

		decodeErr := decoder.Decode(&entry)
		if errors.Is(decodeErr, io.EOF) {
			return nil
		}

		if decodeErr != nil {
			return fmt.Errorf("failed to decode manifest entry: %w", decodeErr)
		}

		if err := validateEntry(&entry); err != nil {
			return err
		}

		if err := visit(entry); err != nil {
			return err
		}
	}
}

func validateEntry(entry *Entry) error {
	if entry.Path == "" {
		return errors.New("manifest contains an empty path")
	}

	if strings.ContainsRune(entry.Path, 0) {
		return fmt.Errorf("manifest path %q contains a NUL byte", entry.Path)
	}

	if err := validateEntryPath(entry); err != nil {
		return err
	}

	return validateEntryMetadata(entry)
}

func validateEntryMetadata(entry *Entry) error {
	if !isValidEntryType(entry.Type) {
		return fmt.Errorf("manifest path %q has invalid type %q", entry.Path, entry.Type)
	}

	if entry.Mode&^fullModeMask != 0 {
		return fmt.Errorf("manifest path %q has invalid mode %#o", entry.Path, entry.Mode)
	}

	if entry.UID < 0 || entry.GID < 0 {
		return fmt.Errorf("manifest path %q has negative owner %d:%d", entry.Path, entry.UID, entry.GID)
	}

	return nil
}

func isValidEntryType(entryType string) bool {
	return entryType == TypeDir || entryType == TypeFile || entryType == TypeLink
}

func validateEntryPath(entry *Entry) error {
	if entry.Path == "." {
		if entry.Type != TypeDir {
			return fmt.Errorf("manifest root entry must be a directory, got %q", entry.Type)
		}

		return nil
	}

	if strings.HasPrefix(entry.Path, "/") || strings.HasSuffix(entry.Path, "/") {
		return fmt.Errorf("manifest path %q must be relative without leading or trailing slashes", entry.Path)
	}

	if path.Clean(entry.Path) != entry.Path {
		return fmt.Errorf("manifest path %q is not canonical", entry.Path)
	}

	if entry.Path == ".." || strings.HasPrefix(entry.Path, "../") {
		return fmt.Errorf("manifest path %q escapes root", entry.Path)
	}

	if entry.Path == ManifestFileName {
		return fmt.Errorf("manifest contains reserved path %s", ManifestFileName)
	}

	return nil
}

func resolveMountRel(dir, mountRoot string) (string, error) {
	rel, err := filepath.Rel(mountRoot, dir)
	if err != nil {
		return "", fmt.Errorf("target %q cannot be resolved relative to mount root %q: %w", dir, mountRoot, err)
	}

	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("target %q escapes mount root %q", dir, mountRoot)
	}

	return rel, nil
}

func openSourceRoot(rootDir, mountRoot string) (*os.Root, bool, error) {
	if mountRoot == "" {
		root, err := os.OpenRoot(rootDir)
		if err != nil {
			return nil, false, fmt.Errorf("failed to open root directory %q: %w", rootDir, err)
		}

		return root, false, nil
	}

	rel, err := resolveMountRel(rootDir, mountRoot)
	if err != nil {
		return nil, false, err
	}

	mount, err := os.OpenRoot(mountRoot)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open mount root %q: %w", mountRoot, err)
	}

	if rel == "." {
		// Skip top-level lost+found only at the volume root so user subpaths keep any
		// lost+found directories they own.
		return mount, true, nil
	}

	subRoot, err := mount.OpenRoot(rel)
	_ = mount.Close()

	if err != nil {
		return nil, false, fmt.Errorf("failed to open source subpath %q: %w", rel, err)
	}

	return subRoot, false, nil
}

func openTargetRoot(targetDir, mountRoot string) (*os.Root, error) {
	if mountRoot == "" {
		if err := os.MkdirAll(targetDir, dirCreatePerm); err != nil {
			return nil, fmt.Errorf("failed to create target directory %q: %w", targetDir, err)
		}

		return os.OpenRoot(targetDir)
	}

	rel, err := resolveMountRel(targetDir, mountRoot)
	if err != nil {
		return nil, err
	}

	mount, err := os.OpenRoot(mountRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to open mount root %q: %w", mountRoot, err)
	}

	if rel == "." {
		return mount, nil
	}

	if err := mount.MkdirAll(rel, dirCreatePerm); err != nil {
		_ = mount.Close()

		return nil, fmt.Errorf("failed to create target subpath %q: %w", rel, err)
	}

	subRoot, err := mount.OpenRoot(rel)
	_ = mount.Close()

	if err != nil {
		return nil, fmt.Errorf("failed to open target subpath %q: %w", rel, err)
	}

	return subRoot, nil
}

func ensureDirectories(root *os.Root, dirs []Entry, opts ApplyOptions) error {
	sorted := slices.Clone(dirs)
	slices.SortFunc(sorted, comparePathsTopDown)

	for _, entry := range sorted {
		if err := ensureDirectory(root, entry, opts); err != nil {
			return err
		}
	}

	return nil
}

func ensureDirectory(root *os.Root, entry Entry, opts ApplyOptions) error {
	skipped, err := createDirectoryIfNeeded(root, entry.Path, opts.NonRoot)
	if err != nil || skipped {
		return err
	}

	// A prior restore may have left a directory read-only (for example 0500), so make
	// it owner-writable during traversal before Pass 3 restores its exact mode.
	info, statErr := root.Lstat(entry.Path)
	if statErr != nil {
		return fmt.Errorf("failed to stat directory %q: %w", entry.Path, statErr)
	}

	if !info.IsDir() || info.Mode().Perm()&ownerRWXPerm == ownerRWXPerm {
		return nil
	}

	chmodErr := root.Chmod(entry.Path, info.Mode().Perm()|ownerRWXPerm)
	if chmodErr != nil && ((!opts.NoChown && !opts.NonRoot) || !errors.Is(chmodErr, fs.ErrPermission)) {
		return fmt.Errorf("failed to prepare directory %q for traversal: %w", entry.Path, chmodErr)
	}

	return nil
}

func createDirectoryIfNeeded(root *os.Root, relPath string, nonRoot bool) (bool, error) {
	if relPath == "." {
		return false, nil
	}

	err := root.MkdirAll(relPath, dirCreatePerm)
	if err == nil {
		return false, nil
	}

	if canIgnoreRootDirPermError(relPath, nonRoot, err) {
		return true, nil
	}

	return false, fmt.Errorf("failed to create directory %q: %w", relPath, err)
}

// In --non-root pods kubelet creates /data and ext4 creates lost+found root-owned,
// so a non-root process cannot mkdir or chmod them.
func canIgnoreRootDirPermError(relPath string, nonRoot bool, err error) bool {
	return nonRoot && (relPath == "." || relPath == lostFoundDir) && errors.Is(err, fs.ErrPermission)
}

func applyItem(root *os.Root, entry Entry, opts ApplyOptions) error {
	info, err := root.Lstat(entry.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", entry.Path, err)
	}

	actualType, ok := classifyEntry(info.Mode())
	if !ok || actualType != entry.Type {
		return nil
	}

	if err := applyOwnership(root, entry, opts); err != nil {
		return err
	}

	// Symlinks carry no portable mode bits and os.Root has no no-follow Chtimes
	// (Root.Chtimes follows links and fails on dangling or external targets).
	if entry.Type != TypeFile {
		return nil
	}

	return applyFileAttrs(root, entry, info, opts)
}

func applyOwnership(root *os.Root, entry Entry, opts ApplyOptions) error {
	if opts.NoChown || opts.NonRoot {
		return nil
	}

	if err := root.Lchown(entry.Path, entry.UID, entry.GID); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("failed to chown %q (use --no-chown to keep destination ownership): %w", entry.Path, err)
		}

		return fmt.Errorf("failed to chown %q: %w", entry.Path, err)
	}

	return nil
}

func applyFileAttrs(root *os.Root, entry Entry, info fs.FileInfo, opts ApplyOptions) error {
	// Chmod runs after Lchown because chown clears setuid and setgid bits on Linux.
	// When ownership was skipped and the mode already matches, skip Chmod so
	// non-root restores succeed on group-writable files owned by another user.
	if !modeAlreadyMatches(info.Mode(), entry.Mode, opts) {
		if err := root.Chmod(entry.Path, unixToFileMode(entry.Mode)); err != nil {
			return fmt.Errorf("failed to chmod %q: %w", entry.Path, err)
		}
	}

	if shouldSkipModTime(info.ModTime(), entry.ModTime) {
		return nil
	}

	if err := root.Chtimes(entry.Path, entry.ModTime, entry.ModTime); err != nil {
		return fmt.Errorf("failed to set mtime on %q: %w", entry.Path, err)
	}

	return nil
}

func applyDirectories(root *os.Root, dirs []Entry, opts ApplyOptions) error {
	sorted := slices.Clone(dirs)
	slices.SortFunc(sorted, comparePathsBottomUp)

	for _, entry := range sorted {
		if err := applyDirectoryEntry(root, entry, opts); err != nil {
			return err
		}
	}

	return nil
}

func applyDirectoryEntry(root *os.Root, entry Entry, opts ApplyOptions) error {
	info, err := root.Lstat(entry.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to stat directory %q: %w", entry.Path, err)
	}

	if !info.IsDir() {
		return nil
	}

	return applyDirectoryMetadata(root, entry, info, opts)
}

func applyDirectoryMetadata(root *os.Root, entry Entry, info fs.FileInfo, opts ApplyOptions) error {
	if err := applyOwnership(root, entry, opts); err != nil {
		return err
	}

	if !modeAlreadyMatches(info.Mode(), entry.Mode, opts) {
		chmodErr := root.Chmod(entry.Path, unixToFileMode(entry.Mode))
		if chmodErr != nil && !canIgnoreRootDirPermError(entry.Path, opts.NonRoot, chmodErr) {
			return fmt.Errorf("failed to chmod directory %q: %w", entry.Path, chmodErr)
		}
	}

	if opts.NonRoot || shouldSkipModTime(info.ModTime(), entry.ModTime) {
		return nil
	}

	if err := root.Chtimes(entry.Path, entry.ModTime, entry.ModTime); err != nil {
		return fmt.Errorf("failed to set mtime on directory %q: %w", entry.Path, err)
	}

	return nil
}

func shouldSkipModTime(current, want time.Time) bool {
	return want.IsZero() || current.Equal(want)
}

func modeAlreadyMatches(current fs.FileMode, want uint32, opts ApplyOptions) bool {
	return (opts.NoChown || opts.NonRoot) && fileModeToUnix(current) == want
}

func classifyEntry(mode fs.FileMode) (string, bool) {
	switch {
	case mode.IsDir():
		return TypeDir, true
	case mode&fs.ModeSymlink != 0:
		return TypeLink, true
	case mode.IsRegular():
		return TypeFile, true
	default:
		return "", false
	}
}

func fileModeToUnix(mode fs.FileMode) uint32 {
	unixMode := uint32(mode.Perm())

	if mode&fs.ModeSetuid != 0 {
		unixMode |= setuidBit
	}

	if mode&fs.ModeSetgid != 0 {
		unixMode |= setgidBit
	}

	if mode&fs.ModeSticky != 0 {
		unixMode |= stickyBit
	}

	return unixMode
}

func unixToFileMode(unixMode uint32) fs.FileMode {
	mode := fs.FileMode(unixMode &^ specialModeMask)

	if unixMode&setuidBit != 0 {
		mode |= fs.ModeSetuid
	}

	if unixMode&setgidBit != 0 {
		mode |= fs.ModeSetgid
	}

	if unixMode&stickyBit != 0 {
		mode |= fs.ModeSticky
	}

	return mode
}

func comparePathsTopDown(left, right Entry) int {
	if left.Path == right.Path {
		return 0
	}

	if left.Path == "." {
		return -1
	}

	if right.Path == "." {
		return 1
	}

	leftDepth := strings.Count(left.Path, "/")
	rightDepth := strings.Count(right.Path, "/")

	if leftDepth != rightDepth {
		return cmp.Compare(leftDepth, rightDepth)
	}

	return cmp.Compare(left.Path, right.Path)
}

func comparePathsBottomUp(left, right Entry) int {
	return -comparePathsTopDown(left, right)
}
