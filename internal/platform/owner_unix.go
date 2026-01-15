//go:build unix

package platform

import (
	"io/fs"
	"syscall"
)

// FileOwner extracts UID and GID from file info on Unix systems.
func FileOwner(info fs.FileInfo) (uid, gid uint32) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Uid, stat.Gid
	}
	return 0, 0
}
