// Package mixed implements a listener that accepts both SOCKS5 and HTTP
// CONNECT on the same port.
//
// TODO(user): implement in Plan 5. Peek the first byte of each incoming
// connection: 0x05 → SOCKS5, anything else → HTTP CONNECT. Then delegate
// to the corresponding protocol handler.
package mixed
