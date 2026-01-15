//go:build !unix

package platform

import "io/fs"

// FileOwner returns zero UID/GID on non-Unix systems.
func FileOwner(info fs.FileInfo) (uid, gid uint32) {
	return 0, 0
}
