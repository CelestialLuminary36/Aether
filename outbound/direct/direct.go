// Package direct implements an outbound dialer that connects to targets
// directly without any intermediate proxy.
package direct

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
	"github.com/CelestialLuminary36/Aether/outbound"
)

// Direct is a passthrough outbound that dials the requested target address
// using the standard library's net.Dialer.
type Direct struct{}

// New creates a new direct outbound dialer.
func New() outbound.Outbound {
	return &Direct{}
}

// Name returns the protocol identifier for this outbound.
func (d *Direct) Name() string {
	return "direct"
}

// DialContext opens a TCP connection to the target specified in sess.
func (d *Direct) DialContext(ctx context.Context, sess *core.Session) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", sess.TargetAddr)
}
