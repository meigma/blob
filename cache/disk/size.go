package disk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type cacheEntry struct {
	path    string
	size    int64
	modTime time.Time
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func pruneDir(root string, targetBytes int64) (freed int64, remaining int64, err error) {
	if targetBytes < 0 {
		targetBytes = 0
	}

	entries := make([]cacheEntry, 0)
	var total int64

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size := info.Size()
		total += size
		entries = append(entries, cacheEntry{
			path:    path,
			size:    size,
			modTime: info.ModTime(),
		})
		return nil
	})
	if errors.Is(walkErr, os.ErrNotExist) {
		return 0, 0, nil
	}
	if walkErr != nil {
		return 0, 0, walkErr
	}

	remaining = total
	if remaining <= targetBytes {
		return 0, remaining, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].path < entries[j].path
		}
		return entries[i].modTime.Before(entries[j].modTime)
	})

	for _, entry := range entries {
		if remaining <= targetBytes {
			break
		}
		if err := os.Remove(entry.path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return freed, remaining, err
		}
		remaining -= entry.size
		freed += entry.size
	}

	return freed, remaining, nil
}
