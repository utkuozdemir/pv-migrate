package strategy

import (
	"fmt"
	"path"
	"strings"

	"github.com/utkuozdemir/pv-migrate/internal/migration"
)

// ValidatePaths checks the request's PVC paths without needing a cluster, so that
// a bad one is reported as itself.
//
// The strategies resolve the paths again where they use them, but a strategy that
// returns an error is understood as that strategy not having worked, and the
// migrator moves on to the next one and finally reports that they all failed. A
// path is a property of the request rather than of any strategy, and one
// two-release strategy would have installed sshd before reaching the check.
func ValidatePaths(sourcePath, destPath string) error {
	_, _, err := resolveMountPaths(&migration.Request{
		Source: migration.PVCInfo{Path: sourcePath},
		Dest:   migration.PVCInfo{Path: destPath},
	})

	return err
}

// resolveMountPaths resolves both PVC paths of a request against the mount points
// they are attached to inside the container.
func resolveMountPaths(req *migration.Request) (string, string, error) {
	srcPath, err := mountSubPath(srcMountPath, req.Source.Path)
	if err != nil {
		return "", "", fmt.Errorf("invalid --source-path: %w", err)
	}

	destPath, err := mountSubPath(destMountPath, req.Dest.Path)
	if err != nil {
		return "", "", fmt.Errorf("invalid --dest-path: %w", err)
	}

	return srcPath, destPath, nil
}

// mountSubPath resolves a user-supplied --source-path or --dest-path against the
// mount point the PVC is attached to inside the container.
//
// It refuses a path expression that leaves the volume. The value used to be
// concatenated onto the mount point and handed straight to rsync, so
// `--dest-path ../` named the container's root instead of the volume: rsync would
// write there, and with --dest-delete-extraneous-files it would delete from there.
// On the source side the same value copies the container's own filesystem into the
// destination PVC.
//
// The guarantee is lexical, and deliberately so. A symlink already inside the
// volume can still take rsync out of it, since rsync follows a symlinked directory
// named on its command line when the argument carries a trailing slash. Resolving
// that would mean inspecting the volume from inside the pod after it is mounted,
// which is a different and racy exercise, and planting such a symlink needs write
// access to the volume already.
//
// A leading slash is accepted and means the volume root, which is what the
// default value is.
//
// The caller's intent about the trailing slash is preserved, because rsync reads
// `dir` as "the directory" and `dir/` as "its contents". `dir/.` is rsync's other
// spelling of the second one, and cleaning the path away would otherwise turn it
// into the first, silently nesting a directory level in the destination. The root
// always gets one, so that a whole-volume migration copies contents rather than
// putting the mount point inside the destination.
func mountSubPath(mountPath, subPath string) (string, error) {
	resolved := path.Clean(mountPath + "/" + subPath)

	if resolved != mountPath && !strings.HasPrefix(resolved, mountPath+"/") {
		return "", fmt.Errorf("path %q escapes the volume mounted at %s", subPath, mountPath)
	}

	if resolved == mountPath || wantsContents(subPath) {
		resolved += "/"
	}

	return resolved, nil
}

// wantsContents reports whether subPath names a directory's contents rather than
// the directory itself, in either of the two spellings rsync accepts.
func wantsContents(subPath string) bool {
	return strings.HasSuffix(subPath, "/") || strings.HasSuffix(subPath, "/.") || subPath == "."
}
