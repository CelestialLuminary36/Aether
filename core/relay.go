package core

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/CelestialLuminary36/Aether/common/bufpool"
)

// halfCloser matches connections that support a one-way write shutdown.
type halfCloser interface {
	CloseWrite() error
}

// Relay copies traffic between left and right in both directions until
// both sides finish or ctx is canceled.
//
// Returns:
//   - uplink: bytes copied from left to right
//   - downlink: bytes copied from right to left
//   - err: first non-EOF error encountered; nil when both directions end cleanly
//
// Guarantees:
//   - On ctx cancellation, both connections are closed immediately.
//   - On return, both left and right are closed. The caller must not use
//     or close them again.
func Relay(ctx context.Context, left, right net.Conn) (uplink, downlink uint64, err error) {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = left.Close()
			_ = right.Close()
		case <-stop:
		}
	}()

	var (
		up, down atomic.Uint64
		firstErr atomic.Value // stores error
		wg       sync.WaitGroup
	)
	wg.Add(2)

	pipe := func(dst, src net.Conn, counter *atomic.Uint64) {
		defer wg.Done()

		bufPtr := bufpool.Get()
		defer bufpool.Put(bufPtr)

		n, err := io.CopyBuffer(dst, src, bufPtr[:])
		counter.Add(uint64(n))

		if err != nil && !errors.Is(err, io.EOF) && !isBenignCloseErr(err) {
			firstErr.CompareAndSwap(nil, err)
		}

		if hc, ok := dst.(halfCloser); ok {
			_ = hc.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}

	go pipe(right, left, &up)   // uplink: left → right
	go pipe(left, right, &down) // downlink: right → left
	wg.Wait()

	go func() { _ = left.Close() }()
	go func() { _ = right.Close() }()

	if v := firstErr.Load(); v != nil {
		err = v.(error)
	}
	return up.Load(), down.Load(), err
}

func isBenignCloseErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
}
