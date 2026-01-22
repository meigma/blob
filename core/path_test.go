package blob

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"leading slash", "/etc/nginx", "etc/nginx"},
		{"trailing slash", "etc/nginx/", "etc/nginx"},
		{"both slashes", "/etc/nginx/", "etc/nginx"},
		{"empty string", "", "."},
		{"root slash", "/", "."},
		{"dot", ".", "."},
		{"simple", "foo", "foo"},
		{"nested path", "/foo/bar/baz", "foo/bar/baz"},
		{"nested with trailing", "foo/bar/baz/", "foo/bar/baz"},
		// Multiple slashes
		{"multiple leading slashes", "///etc/nginx", "etc/nginx"},
		{"multiple trailing slashes", "etc/nginx///", "etc/nginx"},
		{"multiple slashes both ends", "///etc/nginx///", "etc/nginx"},
		{"only slashes", "///", "."},
		{"internal double slashes", "etc//nginx", "etc/nginx"},
		{"internal multiple slashes", "etc///nginx//conf", "etc/nginx/conf"},
		{"mixed slashes everywhere", "//etc//nginx//", "etc/nginx"},
		// Dot and dotdot segments are preserved (for fs.ValidPath to reject)
		{"dotdot in middle", "a/../b", "a/../b"},
		{"dotdot at start", "../etc", "../etc"},
		{"dotdot only", "..", ".."},
		{"dot in middle", "a/./b", "a/./b"},
		{"dotdot with slashes", "//a//..//b//", "a/../b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
