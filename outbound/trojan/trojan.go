// Package trojan implements the Trojan outbound.
//
// TODO(user): implement in Plan 7. It should implement core.TransportPluggable:
// perform the TLS handshake over the underlying connection, send the
// Trojan handshake (password SHA224 + CRLF + target address), and return
// the wrapped net.Conn. For UDP, wrap packets in Trojan UDP header.
package trojan
