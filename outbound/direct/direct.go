// Package direct implements an outbound dialer that connects to targets
// directly without any intermediate proxy.
package direct

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/CelestialLuminary36/Aether/core"
)

// Direct is a passthrough outbound that dials the requested target using
// the standard library's net.Dialer.
type Direct struct {
	tag string
}

// New creates a new direct outbound with the given tag.
func New(tag string) *Direct {
	return &Direct{tag: tag}
}

func (d *Direct) Tag() string             { return d.tag }
func (d *Direct) Type() string            { return "direct" }
func (d *Direct) Networks() []core.Network { return []core.Network{core.NetworkTCP} }

// DialStream opens a TCP connection to md.Destination.
func (d *Direct) DialStream(ctx context.Context, md *core.Metadata) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", md.Destination.String())
	if err != nil {
		return nil, mapDialError(err)
	}
	return conn, nil
}

// mapDialError translates standard library dial errors into core semantic
// errors so that inbound protocols can produce meaningful failure replies.
func mapDialError(err error) error {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch {
		case errors.Is(opErr.Err, syscall.ECONNREFUSED):
			return core.ErrConnectionRefused
		case errors.Is(opErr.Err, syscall.EHOSTUNREACH):
			return core.ErrHostUnreachable
		case errors.Is(opErr.Err, syscall.ENETUNREACH):
			return core.ErrNetworkUnreachable
		}
	}
	return err
}

// DialPacket is not implemented in Plan 1.
//
// TODO(user): implement UDP direct dial in Plan 4. You will need a
// net.ListenUDP binding and a core.PacketConn adapter.
func (d *Direct) DialPacket(ctx context.Context, md *core.Metadata) (core.PacketConn, error) {
	return nil, core.ErrNetworkNotSupported
}
