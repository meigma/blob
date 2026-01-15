//go:build !unix

package platform

import (
	"errors"
	"io/fs"
	"os"
)

// ErrSymlink is returned when attempting to open a symbolic link.
var ErrSymlink = errors.New("symbolic links not supported")

// OpenFileNoFollow opens a file without following symlinks.
// Returns ErrSymlink if the path is a symbolic link.
func OpenFileNoFollow(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, ErrSymlink
	}
	return root.Open(name)
}
