//go:build unix

package posixmeta

import (
	"io/fs"
	"math"
	"syscall"
)

func fileOwner(info fs.FileInfo) (int, int) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}

	return clampUnixID(stat.Uid), clampUnixID(stat.Gid)
}

func clampUnixID(raw uint32) int {
	if uint64(raw) > uint64(math.MaxInt) {
		return 0
	}

	return int(raw)
}
