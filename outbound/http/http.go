// Package http implements an HTTP CONNECT client outbound.
//
// TODO(user): implement in Plan 7. It should implement core.Outbound,
// connect to an upstream HTTP proxy, send "CONNECT host:port HTTP/1.1",
// handle proxy authentication if configured, and return the resulting
// net.Conn / PacketConn.
package http
