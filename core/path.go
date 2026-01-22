package blob

import "strings"

// NormalizePath converts a user-provided path to fs.ValidPath format.
//
// It performs the following transformations:
//   - Strips leading slashes: "/etc/nginx" → "etc/nginx"
//   - Strips trailing slashes: "etc/nginx/" → "etc/nginx"
//   - Collapses consecutive slashes: "etc//nginx" → "etc/nginx"
//   - Converts empty string to root: "" → "."
//   - Preserves root indicator: "/" → "."
//
// The returned path is suitable for use with Blob methods (Open, Stat, Entry, etc.).
//
// Note: This function does not resolve or validate path elements. Paths
// containing "." or ".." elements are preserved and will be rejected by
// Blob methods via fs.ValidPath.
func NormalizePath(p string) string {
	// Trim all leading and trailing slashes
	p = strings.Trim(p, "/")
	if p == "" {
		return "."
	}

	// Collapse consecutive slashes by splitting and rejoining.
	// This removes empty segments but preserves "." and ".." elements.
	parts := strings.Split(p, "/")
	result := parts[:0] // reuse backing array
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return "."
	}
	return strings.Join(result, "/")
}
