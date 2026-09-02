// Package bufpool provides a generic, typed wrapper around sync.Pool for
// reusing fixed-size byte buffers, which helps reduce GC pressure during
// high-throughput relay operations.
package bufpool

import "sync"

// BufferSize is the size, in bytes, of each pooled buffer.
const BufferSize = 32 * 1024

// Pool is a typed wrapper around sync.Pool for reusing objects of type T.
type Pool[T any] struct {
	internal sync.Pool
}

// NewPool creates a new typed pool. The provided newFn is invoked when the
// pool needs to allocate a fresh value.
func NewPool[T any](newFn func() *T) *Pool[T] {
	return &Pool[T]{
		internal: sync.Pool{
			New: func() any { return newFn() },
		},
	}
}

// Get retrieves an item from the pool. The returned value is not zeroed and
// may contain data from a previous use.
func (p *Pool[T]) Get() *T {
	return p.internal.Get().(*T)
}

// Put returns an item to the pool so it can be reused.
func (p *Pool[T]) Put(x *T) {
	p.internal.Put(x)
}

// globalPool is the process-wide pool of 32 KiB byte buffers.
var globalPool = NewPool(func() *[BufferSize]byte {
	var buf [BufferSize]byte
	return &buf
})

// Get returns a 32 KiB buffer from the global pool.
func Get() *[BufferSize]byte {
	return globalPool.Get()
}

// Put returns a 32 KiB buffer to the global pool.
func Put(b *[BufferSize]byte) {
	globalPool.Put(b)
}
