package core

import (
	"context"
	"net"
	"net/netip"
)

// Session carries metadata for a single proxied connection.
type Session struct {
	context.Context

	// SrcAddr is the address of the inbound client.
	SrcAddr net.Addr

	// TargetAddr is the requested destination in "host:port" form.
	TargetAddr string

	// TargetAddrPort is the parsed destination address, if available.
	TargetAddrPort netip.AddrPort

	// Username
	Username string
}

// NewSession creates a Session from the given context, target address, and
// source address. It also attempts to parse the target as a netip.AddrPort.
func NewSession(ctx context.Context, target string, src net.Addr) *Session {
	sess := &Session{
		Context:    ctx,
		SrcAddr:    src,
		TargetAddr: target,
	}

	if ap, err := netip.ParseAddrPort(target); err == nil {
		sess.TargetAddrPort = ap
	}
	return sess
}
