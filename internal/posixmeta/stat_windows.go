//go:build windows

package posixmeta

import "io/fs"

func fileOwner(_ fs.FileInfo) (int, int) {
	return 0, 0
}
