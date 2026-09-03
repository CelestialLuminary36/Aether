// Package socks implements a SOCKS5 client outbound.
//
// TODO(user): implement in Plan 7. It should implement core.Outbound,
// connect to an upstream SOCKS5 server, perform method negotiation and
// auth, send a CONNECT request for md.Destination, and return the
// resulting net.Conn / PacketConn.
package socks
