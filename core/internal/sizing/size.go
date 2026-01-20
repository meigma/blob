// Package sizing provides safe size arithmetic and conversions to prevent overflow.
package sizing

import (
	"io"
	"math"
)

// ToInt converts a uint64 to int, returning overflowErr if it doesn't fit.
func ToInt(size uint64, overflowErr error) (int, error) {
	if size > uint64(math.MaxInt) {
		return 0, overflowErr
	}
	return int(size), nil
}

// ToInt64 converts a uint64 to int64, returning overflowErr if it doesn't fit.
func ToInt64(size uint64, overflowErr error) (int64, error) {
	if size > uint64(math.MaxInt64) {
		return 0, overflowErr
	}
	return int64(size), nil
}

// AddUint64 adds two uint64 values, returning (result, false) on overflow.
func AddUint64(a, b uint64) (uint64, bool) {
	sum := a + b
	if sum < a {
		return 0, false
	}
	return sum, true
}

// ReadAllWithLimit reads up to maxSize bytes from r.
// Returns overflowErr if more than maxSize bytes are available.
func ReadAllWithLimit(r io.Reader, maxSize uint64, overflowErr error) ([]byte, error) {
	if maxSize > uint64(math.MaxInt-1) {
		return nil, overflowErr
	}
	limit := int64(maxSize) + 1 //nolint:gosec // checked above
	lr := &io.LimitedReader{R: r, N: limit}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maxSize { //nolint:gosec // len is always non-negative
		return nil, overflowErr
	}
	return data, nil
}
