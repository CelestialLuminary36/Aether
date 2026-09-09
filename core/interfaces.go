package core

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/CelestialLuminary36/Aether/common/packet"
)

// Dispatcher is the sole traffic scheduler. Inbounds only know about
// the Dispatcher; they do not know about outbounds.
//
// DispatchStream's onDialed callback (may be nil) is called once after
// the outbound dial succeeds but before relay begins. Inbounds use it
// to send protocol-specific success replies (e.g. SOCKS5 REP=0x00 or
// HTTP 200 Connection Established). If onDialed returns an error, the
// whole dispatch is aborted.
//
// TODO(user): decide whether to add per-packet callbacks for stats in
// Plan 10 (observability).
type Dispatcher interface {
	DispatchStream(ctx context.Context, md *Metadata, conn net.Conn, onDialed func() error) error
	DispatchPacket(ctx context.Context, md *Metadata, conn PacketConn) error
}

// Inbound is a listening proxy protocol server.
type Inbound interface {
	// Tag is the configured instance name, e.g. "socks-in".
	Tag() string
	// Type is the protocol name, e.g. "socks".
	Type() string
	Start(ctx context.Context) error
	Close() error
}

// Outbound is a connection builder.
//
// Networks() declares which networks the outbound supports; this is used
// during configuration validation. For unsupported networks,
// DialStream/DialPacket must return ErrNetworkNotSupported.
type Outbound interface {
	Tag() string
	Type() string
	Networks() []Network

	DialStream(ctx context.Context, md *Metadata) (net.Conn, error)
	DialPacket(ctx context.Context, md *Metadata) (PacketConn, error)
}

// RouteStage tells the Router which pass of two-stage routing this is.
// The Dispatcher owns the "resolve already ran" state and passes the
// stage explicitly; it is never inferred from Metadata.
type RouteStage uint8

const (
	StageInitial RouteStage = iota
	StagePostResolve
)

// Router is a pure function: metadata in, decision out.
type Router interface {
	Route(md *Metadata, stage RouteStage) (Decision, error)
}

// PacketConn is a packet-oriented connection.
//
// Unlike net.Conn, a single PacketConn may carry packets to multiple
// destinations; each packet carries its own address.
//
// TODO(user): implement UDP inbound/outbound PacketConn in Plan 4.
type PacketConn interface {
	ReadPacket(buf *packet.Buffer) (dest Addr, err error)
	WritePacket(buf *packet.Buffer, dest Addr) error

	Close() error
	LocalAddr() net.Addr
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

type Resolver interface {
	Lookup(ctx context.Context, md *Metadata) ([]netip.Addr, error)
}

// Sniffer inspects the leading bytes of a connection to guess the
// protocol and destination domain without consuming the stream.
type Sniffer interface {
	Name() string
	Sniff(header []byte) (SniffResult, error)
}

// SniffResult is what a Sniffer learned from a connection's leading bytes.
type SniffResult struct {
	Protocol string // "tls", "http", ...
	Domain   string // TLS SNI or HTTP Host; empty if none
}
