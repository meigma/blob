package write

import (
	"fmt"
	"io/fs"
	"os"
)

// CheckFileUnchanged verifies a file wasn't modified during write.
// In strict mode, it compares size, mtime, and permissions before/after.
func CheckFileUnchanged(f *os.File, path string, before fs.FileInfo, strict bool) error {
	if !strict {
		return nil
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode().Perm() != before.Mode().Perm() {
		return fmt.Errorf("file changed during archive creation: %s", path)
	}
	return nil
}

// ValidateFileInfo checks file info consistency in strict mode.
// Returns an error if info is nil or doesn't match finfo.
func ValidateFileInfo(path string, info, finfo fs.FileInfo, strict bool) error {
	if !strict {
		return nil
	}
	if info == nil {
		return fmt.Errorf("missing file info: %s", path)
	}
	if !os.SameFile(info, finfo) {
		return fmt.Errorf("file changed during archive creation: %s", path)
	}
	return nil
}

// ResolveEntryInfo gets FileInfo from a DirEntry, filtering out symlinks
// and non-regular files. Returns (info, ok, error) where ok=false means
// the entry should be skipped.
func ResolveEntryInfo(root *os.Root, fsPath string, d fs.DirEntry, strict bool) (fs.FileInfo, bool, error) {
	dtype := d.Type()
	if dtype&fs.ModeSymlink != 0 {
		return nil, false, nil
	}

	if dtype == 0 {
		linfo, err := root.Lstat(fsPath)
		if err != nil {
			return nil, false, err
		}
		if linfo.Mode()&fs.ModeSymlink != 0 {
			return nil, false, nil
		}
		if !linfo.Mode().IsRegular() {
			return nil, false, nil
		}
		return linfo, true, nil
	}

	if !dtype.IsRegular() {
		return nil, false, nil
	}
	if !strict {
		return nil, true, nil
	}

	info, err := d.Info()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	return info, true, nil
}
