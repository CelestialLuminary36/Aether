// Package inbound defines the interface implemented by inbound proxy listeners
// such as SOCKS5.
package inbound

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// Handler is called for each accepted inbound connection after the protocol
// handshake has been completed. The handler owns inConn and must close it.
type Handler func(sess *core.Session, inConn net.Conn)

// Inbound represents a listening proxy protocol server.
type Inbound interface {
	// Name returns a short identifier for this inbound protocol (e.g. "socks5").
	Name() string

	// Start begins accepting connections and dispatching them to handler.
	// It blocks until the supplied context is canceled or an unrecoverable
	// error occurs.
	Start(ctx context.Context, handler Handler) error

	// Close stops the listener and releases associated resources.
	Close() error
}
