package bucketstorage_test

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/bucketstorage"
)

// FuzzValidateSubpath checks the one thing --path validation exists for: a path
// it accepts must stay inside the volume, because the accepted value is joined
// onto the mount root and handed to rclone as the local side of a sync. A value
// that climbed out would let a restore write outside the PVC it was told to write
// to, and a backup read the rclone container's own filesystem.
//
// The property is stated against path.Join rather than against a list of bad
// inputs, so it covers escapes nobody thought to enumerate.
func FuzzValidateSubpath(f *testing.F) {
	f.Add("")
	f.Add("sub")
	f.Add("sub/dir")
	f.Add("a/b/../c")
	f.Add("..")
	f.Add("../etc")
	f.Add("/absolute")
	f.Add("//absolute")
	f.Add("sub/../..")
	f.Add("./..")
	f.Add("...")
	f.Add("a/./b")

	f.Fuzz(func(t *testing.T, subpath string) {
		if err := bucketstorage.ValidateSubpath(subpath); err != nil {
			return
		}

		const mountRoot = "/data"

		joined := path.Join(mountRoot, subpath)

		require.True(t, joined == mountRoot || strings.HasPrefix(joined, mountRoot+"/"),
			"accepted subpath %q joins to %q, which is outside %q", subpath, joined, mountRoot)
	})
}

// FuzzValidatePrefix checks that an accepted prefix contributes exactly the path
// segments it looks like it contributes. The prefix is concatenated into the
// remote object path, so a prefix that collapsed, doubled a separator or carried
// a traversal segment would silently move every backup somewhere else in the
// bucket, and a later restore would read a different place than the backup wrote.
func FuzzValidatePrefix(f *testing.F) {
	f.Add("")
	f.Add("pv-migrate")
	f.Add("team/env/app")
	f.Add("/leading")
	f.Add("trailing/")
	f.Add("double//separator")
	f.Add("..")
	f.Add("a/../b")
	f.Add(".")

	f.Fuzz(func(t *testing.T, prefix string) {
		if err := bucketstorage.ValidatePrefix(prefix); err != nil {
			return
		}

		if prefix == "" {
			return
		}

		require.Equal(t, prefix, path.Clean(prefix),
			"accepted prefix %q is not already in its cleaned form, so the object path it "+
				"produces differs from the one it reads as", prefix)

		for segment := range strings.SplitSeq(prefix, "/") {
			require.NotEmpty(t, segment, "accepted prefix %q has an empty segment", prefix)
			require.NotEqual(t, ".", segment, "accepted prefix %q has a no-op segment", prefix)
			require.NotEqual(t, "..", segment, "accepted prefix %q has a traversal segment", prefix)
		}
	})
}
