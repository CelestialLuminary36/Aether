// Package dispatcher implements core.Dispatcher.
//
// Two implementations exist:
//   - StaticDispatcher (Plan 1): no routing; every stream goes to the
//     primary outbound.
//   - PipelineDispatcher (Plan 2): the routed runtime pipeline, built
//     from explicit dependencies (core.Router, core.Resolver,
//     OutboundRegistry, optional core.Sniffers).
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

type PipelineDispatcher struct {
	router   core.Router
	resolver core.Resolver
	registry *OutboundRegistry
	sniffers []core.Sniffer
}

type Option func(*PipelineDispatcher)

// NewStaticDispatcher creates a StaticDispatcher. block may be nil.
func NewStaticDispatcher(primary core.Outbound, block core.Outbound) *StaticDispatcher {
	return &StaticDispatcher{primary: primary, block: block}
}

func NewPipelineDispatcher(router core.Router, resolver core.Resolver,
	reg *OutboundRegistry, opts ...Option) *PipelineDispatcher {
	return &PipelineDispatcher{router: router, resolver: resolver, registry: reg}
}

func WithSniffers(s ...core.Sniffer) Option {
	return func(d *PipelineDispatcher) {
		d.sniffers = append(d.sniffers, s...)
	}
}

// DispatchStream dials the primary outbound, calls onDialed, then relays.
//
// Ownership of outConn is transferred to core.Relay; Relay will close
// both inConn and outConn on return. The dispatcher must not close
// outConn again.
//
// StaticDispatcher carries no routing; PipelineDispatcher replaces it
// once stream dispatch is implemented (Plan 2 Task 4).
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
