package file

import (
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// DecompressPool manages reusable zstd decoders to reduce allocation overhead.
type DecompressPool struct {
	pool             *sync.Pool
	maxDecoderMemory uint64
}

// NewDecompressPool creates a new pool for zstd decoders.
// If maxMemory is 0, no memory limit is applied to decoders.
func NewDecompressPool(maxMemory uint64) *DecompressPool {
	p := &DecompressPool{
		maxDecoderMemory: maxMemory,
	}
	p.pool = &sync.Pool{
		New: func() any {
			dec, err := p.newDecoder(nil)
			if err != nil {
				return nil
			}
			return dec
		},
	}
	return p
}

// Get returns a decoder configured to read from r.
// The caller must call the returned release function when done.
// If an error is returned, no release function needs to be called.
func (p *DecompressPool) Get(r io.Reader) (*zstd.Decoder, func(), error) {
	if p == nil || p.pool == nil {
		// No pool available, create a one-off decoder
		dec, err := p.newDecoder(r)
		if err != nil {
			return nil, nil, err
		}
		return dec, dec.Close, nil
	}

	value := p.pool.Get()
	if value == nil {
		// Pool's New function failed, try directly
		dec, err := p.newDecoder(r)
		if err != nil {
			return nil, nil, err
		}
		return dec, dec.Close, nil
	}

	dec, ok := value.(*zstd.Decoder)
	if !ok {
		// Unexpected type in pool, create new
		newDec, err := p.newDecoder(r)
		if err != nil {
			return nil, nil, err
		}
		return newDec, newDec.Close, nil
	}

	if err := dec.Reset(r); err != nil {
		// Reset failed, close this one and create new
		dec.Close()
		newDec, err := p.newDecoder(r)
		if err != nil {
			return nil, nil, err
		}
		return newDec, newDec.Close, nil
	}

	// Return decoder with release function that returns it to pool
	return dec, func() {
		_ = dec.Reset(nil) //nolint:errcheck // clearing state before pool return
		p.pool.Put(dec)
	}, nil
}

// newDecoder creates a new zstd decoder with the configured memory limit.
func (p *DecompressPool) newDecoder(r io.Reader) (*zstd.Decoder, error) {
	if p == nil || p.maxDecoderMemory == 0 {
		return zstd.NewReader(r)
	}
	return zstd.NewReader(r, zstd.WithDecoderMaxMemory(p.maxDecoderMemory))
}
