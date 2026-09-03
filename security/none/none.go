// Package none implements a no-op security layer.
//
// TODO(user): implement in Plan 7. It should wrap a net.Conn as a
// pass-through so that TransportPluggable protocols can be used over
// plain TCP without TLS.
package none
