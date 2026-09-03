// Package dispatcher implements core.Dispatcher.
//
// Plan 1 provides StaticDispatcher: no sniffing, no routing, no DNS.
// Every stream is sent to the primary outbound. Later plans will extend
// this into the full pipeline described in the spec.
package dispatcher

import (
	"context"
	"fmt"
	"net"

	"github.com/CelestialLuminary36/Aether/core"
)

// StaticDispatcher sends every DispatchStream to the primary outbound.
type StaticDispatcher struct {
	primary core.Outbound
	// block is reserved for Plan 2, when ActionBlock will use it.
	block core.Outbound
}

// New creates a StaticDispatcher. block may be nil.
func New(primary core.Outbound, block core.Outbound) *StaticDispatcher {
	return &StaticDispatcher{primary: primary, block: block}
}

// DispatchStream dials the primary outbound, calls onDialed, then relays.
//
// Ownership of outConn is transferred to core.Relay; Relay will close
// both inConn and outConn on return. The dispatcher must not close
// outConn again.
//
// TODO(user): when Plan 2 adds routing, select outbound based on
// md.Destination / SniffedDomain / ResolvedIPs instead of always using
// primary.
func (d *StaticDispatcher) DispatchStream(
	ctx context.Context,
	md *core.Metadata,
	inConn net.Conn,
	onDialed func() error,
) error {
	outConn, err := d.primary.DialStream(ctx, md)
	if err != nil {
		return fmt.Errorf("dial primary: %w", err)
	}

	if onDialed != nil {
		if err := onDialed(); err != nil {
			_ = outConn.Close()
			return fmt.Errorf("onDialed: %w", err)
		}
	}

	_, _, err = core.Relay(ctx, inConn, outConn)
	return err
}

// DispatchPacket is not implemented in Plan 1.
//
// TODO(user): implement UDP dispatch in Plan 4. You will need a NAT
// table keyed by (source address, destination address) with idle timeout.
func (d *StaticDispatcher) DispatchPacket(
	ctx context.Context,
	md *core.Metadata,
	inConn core.PacketConn,
) error {
	return core.ErrNetworkNotSupported
}
