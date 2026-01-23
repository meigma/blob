package batch

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockByteSource provides an in-memory ByteSource for testing.
type mockByteSource struct {
	data     []byte
	sourceID string
}

func (m *mockByteSource) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (m *mockByteSource) Size() int64 {
	return int64(len(m.data))
}

func (m *mockByteSource) SourceID() string {
	if m.sourceID == "" {
		sum := sha256.Sum256(m.data)
		m.sourceID = "mock:" + hex.EncodeToString(sum[:])
	}
	return m.sourceID
}

// mockSink captures processed entries for testing.
type mockSink struct {
	shouldProcess func(*Entry) bool
	written       map[string][]byte
	errors        map[string]error
}

func newMockSink() *mockSink {
	return &mockSink{
		shouldProcess: func(*Entry) bool { return true },
		written:       make(map[string][]byte),
		errors:        make(map[string]error),
	}
}

func (s *mockSink) ShouldProcess(entry *Entry) bool {
	return s.shouldProcess(entry)
}

func (s *mockSink) Writer(entry *Entry) (Committer, error) {
	if err, ok := s.errors[entry.Path]; ok {
		return nil, err
	}
	return &mockCommitter{sink: s, path: entry.Path}, nil
}

type mockCommitter struct {
	sink *mockSink
	path string
	data []byte
}

func (c *mockCommitter) Write(p []byte) (int, error) {
	c.data = append(c.data, p...)
	return len(p), nil
}

func (c *mockCommitter) Commit() error {
	c.sink.written[c.path] = c.data
	return nil
}

func (c *mockCommitter) Discard() error {
	return nil
}

func TestGroupAdjacentEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entries  []*Entry
		expected []rangeGroup
	}{
		{
			name: "single entry",
			entries: []*Entry{
				{Path: "a", DataOffset: 0, DataSize: 10},
			},
			expected: []rangeGroup{
				{start: 0, end: 10, entries: []*Entry{{Path: "a", DataOffset: 0, DataSize: 10}}},
			},
		},
		{
			name: "adjacent entries",
			entries: []*Entry{
				{Path: "a", DataOffset: 0, DataSize: 10},
				{Path: "b", DataOffset: 10, DataSize: 20},
				{Path: "c", DataOffset: 30, DataSize: 15},
			},
			expected: []rangeGroup{
				{start: 0, end: 45, entries: []*Entry{
					{Path: "a", DataOffset: 0, DataSize: 10},
					{Path: "b", DataOffset: 10, DataSize: 20},
					{Path: "c", DataOffset: 30, DataSize: 15},
				}},
			},
		},
		{
			name: "gap between entries",
			entries: []*Entry{
				{Path: "a", DataOffset: 0, DataSize: 10},
				{Path: "b", DataOffset: 20, DataSize: 10}, // gap at 10-20
			},
			expected: []rangeGroup{
				{start: 0, end: 10, entries: []*Entry{{Path: "a", DataOffset: 0, DataSize: 10}}},
				{start: 20, end: 30, entries: []*Entry{{Path: "b", DataOffset: 20, DataSize: 10}}},
			},
		},
		{
			name: "multiple groups",
			entries: []*Entry{
				{Path: "a", DataOffset: 0, DataSize: 10},
				{Path: "b", DataOffset: 10, DataSize: 10},
				{Path: "c", DataOffset: 50, DataSize: 10},
				{Path: "d", DataOffset: 60, DataSize: 10},
			},
			expected: []rangeGroup{
				{start: 0, end: 20, entries: []*Entry{
					{Path: "a", DataOffset: 0, DataSize: 10},
					{Path: "b", DataOffset: 10, DataSize: 10},
				}},
				{start: 50, end: 70, entries: []*Entry{
					{Path: "c", DataOffset: 50, DataSize: 10},
					{Path: "d", DataOffset: 60, DataSize: 10},
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups := groupAdjacentEntries(tc.entries)

			require.Len(t, groups, len(tc.expected))
			for i, g := range groups {
				assert.Equal(t, tc.expected[i].start, g.start, "group %d start", i)
				assert.Equal(t, tc.expected[i].end, g.end, "group %d end", i)
				require.Len(t, g.entries, len(tc.expected[i].entries), "group %d entries", i)
				for j, e := range g.entries {
					assert.Equal(t, tc.expected[i].entries[j].Path, e.Path, "group %d entry %d path", i, j)
				}
			}
		})
	}
}

func TestProcessor_ShouldProcess(t *testing.T) {
	t.Parallel()

	sink := newMockSink()
	sink.shouldProcess = func(e *Entry) bool {
		return e.Path != "skip.txt"
	}

	// Create test entries (uncompressed for simplicity)
	entries := []*Entry{
		{Path: "a.txt", DataOffset: 0, DataSize: 5, OriginalSize: 5, Hash: sha256Hash("hello"), Compression: CompressionNone},
		{Path: "skip.txt", DataOffset: 5, DataSize: 5, OriginalSize: 5, Hash: sha256Hash("world"), Compression: CompressionNone},
	}

	// Set up source data
	source := &mockByteSource{data: []byte("helloworld")}
	proc := NewProcessor(source, nil, 0)

	stats, err := proc.Process(entries, sink)
	require.NoError(t, err)

	// Only a.txt should be written
	assert.Contains(t, sink.written, "a.txt")
	assert.NotContains(t, sink.written, "skip.txt")
	assert.Equal(t, []byte("hello"), sink.written["a.txt"])

	// Verify stats
	assert.Equal(t, 1, stats.Processed)
	assert.Equal(t, 1, stats.Skipped)
	assert.Equal(t, uint64(5), stats.TotalBytes)
}

func TestProcessor_EmptyEntries(t *testing.T) {
	t.Parallel()

	sink := newMockSink()
	proc := NewProcessor(&mockByteSource{data: make([]byte, 100)}, nil, 0)

	stats, err := proc.Process(nil, sink)
	assert.NoError(t, err)
	assert.Equal(t, ProcessStats{}, stats)

	stats, err = proc.Process([]*Entry{}, sink)
	assert.NoError(t, err)
	assert.Equal(t, ProcessStats{}, stats)
}

// sha256Hash returns a valid SHA256 hash for the given content.
func sha256Hash(content string) []byte {
	h := sha256.Sum256([]byte(content))
	return h[:]
}
