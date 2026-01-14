// Package pathutil provides path manipulation for slash-separated archive paths.
package pathutil

import "strings"

// Base returns the last element of a slash-separated path.
// If path is empty or ".", it returns ".".
func Base(path string) string {
	if path == "" || path == "." {
		return "."
	}
	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// DirPrefix converts a path to its directory prefix form.
// For ".", returns "" (empty prefix matches all).
// For other paths, appends "/" to match children.
func DirPrefix(name string) string {
	if name == "." {
		return ""
	}
	return name + "/"
}

// Child extracts the immediate child name from a full path given a prefix.
// Returns the child name and whether it's a subdirectory (has more path components).
// If path doesn't have the prefix, behavior is undefined.
func Child(path, prefix string) (name string, isSubDir bool) {
	relPath := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(relPath, "/"); idx >= 0 {
		return relPath[:idx], true
	}
	return relPath, false
}
