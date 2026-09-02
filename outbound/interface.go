// Package outbound defines the interface implemented by outbound proxy
// dialers such as the direct dialer.
package outbound

import (
	"context"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// Outbound represents a dialer that establishes connections to target hosts.
type Outbound interface {
	// Name returns a short identifier for this outbound strategy (e.g. "direct").
	Name() string

	// DialContext opens a connection to the target described by sess, honoring
	// the supplied context for cancellation and timeouts.
	DialContext(ctx context.Context, sess *core.Session) (net.Conn, error)
}
