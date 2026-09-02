package core

import (
	"io"
	"net"
	"sync"

	"github.com/CelestialLuminary36/Aether/common/bufpool"
)

// halfCloser matches connections that support a one-way write shutdown,
// such as *net.TCPConn. Closing only the write side lets the peer receive
// any remaining buffered data before the connection terminates.
type halfCloser interface {
	CloseWrite() error
}

// Relay copies traffic between left and right in both directions until both
// sides complete. It uses a pooled buffer to avoid per-relay allocations.
func Relay(left, right net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// pipe copies from src to dst and then signals the write half-close when done.
	pipe := func(dst, src net.Conn) {
		defer wg.Done()

		bufPtr := bufpool.Get()
		defer bufpool.Put(bufPtr)

		_, _ = io.CopyBuffer(dst, src, bufPtr[:])

		if hc, ok := dst.(halfCloser); ok {
			_ = hc.CloseWrite()
		}
	}

	go pipe(left, right)
	go pipe(right, left)
	wg.Wait()
}
