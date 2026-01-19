//go:build unix

package platform

import (
	"errors"
	"os"
	"syscall"
)

// ErrSymlink is returned when attempting to open a symbolic link.
var ErrSymlink = errors.New("symbolic links not supported")

// OpenFileNoFollow opens a file without following symlinks.
// Returns ErrSymlink if the path is a symbolic link.
func OpenFileNoFollow(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, ErrSymlink
		}
		return nil, err
	}
	return f, nil
}
