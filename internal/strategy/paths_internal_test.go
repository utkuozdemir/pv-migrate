package strategy

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMountSubPath covers both halves of what the resolved path has to get right:
// staying inside the volume, and keeping the trailing slash that tells rsync to
// copy a directory's contents rather than the directory itself.
func TestMountSubPath(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		subPath string
		want    string
	}{
		"default root":       {subPath: "/", want: "/source/"},
		"empty":              {subPath: "", want: "/source/"},
		"subdirectory":       {subPath: "sub", want: "/source/sub"},
		"subdirectory slash": {subPath: "sub/", want: "/source/sub/"},
		"leading slash":      {subPath: "/sub", want: "/source/sub"},
		"leading slash both": {subPath: "/sub/", want: "/source/sub/"},
		"nested":             {subPath: "a/b/c", want: "/source/a/b/c"},
		"interior dotdot":    {subPath: "a/../b", want: "/source/b"},
		"interior dot":       {subPath: "a/./b", want: "/source/a/b"},
		"redundant slashes":  {subPath: "a//b", want: "/source/a/b"},
		"back to root":       {subPath: "a/..", want: "/source/"},
		"path with space":    {subPath: "my dir/", want: "/source/my dir/"},
		"dotdot named dir":   {subPath: "...", want: "/source/..."},
		// rsync reads "dir/." the same as "dir/", so cleaning it down to "dir" would
		// quietly nest a directory level in the destination.
		"trailing dot":        {subPath: "sub/.", want: "/source/sub/"},
		"trailing dot slash":  {subPath: "sub/./", want: "/source/sub/"},
		"nested trailing dot": {subPath: "a/b/.", want: "/source/a/b/"},
		"bare dot":            {subPath: ".", want: "/source/"},
		"escape":              {subPath: "..", want: ""},
		"escape trailing":     {subPath: "../", want: ""},
		"escape deep":         {subPath: "../../etc", want: ""},
		"escape via subdir":   {subPath: "a/../..", want: ""},
		"escape absolute":     {subPath: "/../etc", want: ""},
		"escape sibling":      {subPath: "../sourceother", want: ""},
		"escape then descend": {subPath: "../dest/", want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := mountSubPath(srcMountPath, tc.subPath)

			if tc.want == "" {
				require.ErrorContains(t, err, "escapes the volume")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			// Whatever was returned has to still be inside the volume once the shell
			// and rsync have resolved it, which is the property the escape check exists
			// for. Stated against path.Clean so it also covers the cases above that are
			// only listed as expected strings.
			cleaned := path.Clean(got)
			assert.True(t, cleaned == srcMountPath || strings.HasPrefix(cleaned, srcMountPath+"/"),
				"resolved path %q is outside %q", got, srcMountPath)
		})
	}
}

// TestMountSubPathRejectsEscapeForDest guards the more damaging direction: the
// destination is written to, and with --dest-delete-extraneous-files it is also
// deleted from.
func TestMountSubPathRejectsEscapeForDest(t *testing.T) {
	t.Parallel()

	_, err := mountSubPath(destMountPath, "../")
	require.ErrorContains(t, err, "escapes the volume")
}
