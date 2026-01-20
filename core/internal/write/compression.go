package write

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// SkipCompressionFunc returns true when a file should be stored uncompressed.
// It is called once per file and should be inexpensive.
type SkipCompressionFunc func(path string, info fs.FileInfo) bool

// DefaultSkipCompression returns a SkipCompressionFunc that skips small files
// and known already-compressed extensions.
func DefaultSkipCompression(minSize int64) SkipCompressionFunc {
	return func(path string, info fs.FileInfo) bool {
		if info != nil && minSize > 0 && info.Size() < minSize {
			return true
		}
		ext := strings.ToLower(filepath.Ext(path))
		_, ok := defaultSkipCompressionExts[ext]
		return ok
	}
}

// ShouldSkip checks if any predicate returns true for the given file.
func ShouldSkip(path string, info fs.FileInfo, predicates []SkipCompressionFunc) bool {
	for _, fn := range predicates {
		if fn == nil {
			continue
		}
		if fn(path, info) {
			return true
		}
	}
	return false
}

var defaultSkipCompressionExts = map[string]struct{}{
	".7z":    {},
	".aac":   {},
	".avif":  {},
	".br":    {},
	".bz2":   {},
	".flac":  {},
	".gif":   {},
	".gz":    {},
	".heic":  {},
	".ico":   {},
	".jpeg":  {},
	".jpg":   {},
	".m4v":   {},
	".mkv":   {},
	".mov":   {},
	".mp3":   {},
	".mp4":   {},
	".ogg":   {},
	".opus":  {},
	".pdf":   {},
	".png":   {},
	".rar":   {},
	".tgz":   {},
	".wav":   {},
	".webm":  {},
	".webp":  {},
	".woff":  {},
	".woff2": {},
	".xz":    {},
	".zip":   {},
	".zst":   {},
}
