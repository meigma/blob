//go:build !unix

package blob

import (
	"io/fs"
	"os"
)

// fileOwner returns zero UID/GID on non-Unix systems.
func fileOwner(info fs.FileInfo) (uid, gid uint32) {
	return 0, 0
}

func openFileNoFollow(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, ErrSymlink
	}
	return root.Open(name)
}
