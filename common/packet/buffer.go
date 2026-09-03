// Package packet provides a byte buffer with headroom for UDP and other
// packet-oriented protocols.
//
// Implementations like Shadowsocks and Trojan need to prepend headers
// (address, nonce) to each packet. A plain []byte would require a new
// allocation plus a copy for every packet. Buffer keeps a fixed backing
// array and uses start/end cursors, allowing header space to be claimed
// in front of the payload without copying.
package packet

// Buffer is a byte buffer with reserved headroom.
//
// The underlying array is fixed. Valid data is data[start:end]. Reset
// places the cursors at the midpoint so both ExtendHeader and Advance
// have room.
type Buffer struct {
	data  []byte
	start int
	end   int
}

// New allocates a Buffer with the given total capacity.
func New(size int) *Buffer {
	b := &Buffer{data: make([]byte, size)}
	b.Reset()
	return b
}

// Cap returns the total capacity of the backing array.
func (b *Buffer) Cap() int { return cap(b.data) }

// Len returns the length of the valid data region.
func (b *Buffer) Len() int { return b.end - b.start }

// Bytes returns the valid data region.
func (b *Buffer) Bytes() []byte { return b.data[b.start:b.end] }

// Reset clears the buffer and places cursors at the midpoint.
func (b *Buffer) Reset() {
	mid := cap(b.data) / 2
	b.start = mid
	b.end = mid
}

// Advance reserves n bytes at the tail and returns a slice pointing to
// that writable region. Panics if tail room is exhausted.
//
// TODO(user): write a test for the panic case and for normal usage.
func (b *Buffer) Advance(n int) []byte {
	if b.end+n > cap(b.data) {
		panic("packet.Buffer: Advance exceeds capacity")
	}
	old := b.end
	b.end += n
	return b.data[old:b.end]
}

// ExtendHeader reserves n bytes at the head and returns a slice pointing
// to that writable region. Panics if head room is exhausted.
//
// TODO(user): write a test for the prepend behavior and for header
// exhaustion.
func (b *Buffer) ExtendHeader(n int) []byte {
	if b.start-n < 0 {
		panic("packet.Buffer: ExtendHeader exceeds head room")
	}
	b.start -= n
	return b.data[b.start : b.start+n]
}

// Truncate sets the valid data length to n.
//
// TODO(user): add tests for Truncate and Reset restoring headroom.
func (b *Buffer) Truncate(n int) {
	if n < 0 || b.start+n > cap(b.data) {
		panic("packet.Buffer: Truncate out of range")
	}
	b.end = b.start + n
}
