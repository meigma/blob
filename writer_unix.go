//go:build unix

package blob

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// fileOwner extracts UID and GID from file info on Unix systems.
func fileOwner(info fs.FileInfo) (uid, gid uint32) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Uid, stat.Gid
	}
	return 0, 0
}

func openFileNoFollow(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, ErrSymlink
		}
		return nil, err
	}
	return f, nil
}
